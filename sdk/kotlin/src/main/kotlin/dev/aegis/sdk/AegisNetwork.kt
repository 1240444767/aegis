package dev.aegis.sdk

import okhttp3.Dns
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.dnsoverhttps.DnsOverHttps
import java.io.IOException
import java.io.InterruptedIOException
import java.net.ConnectException
import java.net.InetAddress
import java.net.NoRouteToHostException
import java.net.PortUnreachableException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.security.cert.CertificateException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import javax.net.ssl.SSLHandshakeException
import javax.net.ssl.SSLPeerUnverifiedException

/**
 * 网络失败的大类，随 [AegisException.networkReason] 交给调用方。
 *
 * 「网络连接失败」四个字对排查没有用：DNS 被运营商拦、CDN 节点连不上、证书被中间人换掉、
 * 服务端 502 是四种完全不同的问题，用户报上来时至少要能分清是哪一种。
 */
object AegisNetworkReason {
    const val NONE = ""
    /** 域名解析不到（含系统 DNS 与 DoH 全部失败）。 */
    const val DNS = "dns"
    /** 解析到了地址，但所有地址都连不上。 */
    const val CONNECT = "connect"
    /** TLS 握手失败。证书不受信任也在这里，那种情况不会重试。 */
    const val TLS = "tls"
    /** 连上了但等不到响应。 */
    const val TIMEOUT = "timeout"
    /** 网关 / CDN 回了不是平台信封的东西：5xx 错误页、拦截页、空响应。 */
    const val HTTP = "http"
    /** 其他 I/O 错误（连接中途被重置等）。 */
    const val IO = "io"
}

/**
 * 判断一次 I/O 失败发生时，请求是否**一定**还没到服务端。
 *
 * 这决定了非幂等请求能不能自动重发：连接阶段的失败（解析不到、连不上、握手失败）
 * 说明一个字节都没发出去，重发不会造成重复下单、重复刷新令牌；读超时、连接中途被重置
 * 则可能服务端已经处理完、只是响应丢了，这时重发刷新令牌会触发服务端的「令牌复用」
 * 判定，把整条会话链吊销。
 */
internal fun IOException.failedBeforeSending(): Boolean = when (this) {
    is UnknownHostException, is ConnectException, is NoRouteToHostException, is PortUnreachableException -> true
    is SSLHandshakeException -> true
    is SocketTimeoutException -> message.orEmpty().contains("connect", ignoreCase = true)
    else -> false
}

/** 归类到 [AegisNetworkReason]。 */
internal fun IOException.networkReason(): String = when (this) {
    is UnknownHostException -> AegisNetworkReason.DNS
    is ConnectException, is NoRouteToHostException, is PortUnreachableException -> AegisNetworkReason.CONNECT
    is SSLHandshakeException, is SSLPeerUnverifiedException -> AegisNetworkReason.TLS
    is SocketTimeoutException ->
        if (message.orEmpty().contains("connect", ignoreCase = true)) AegisNetworkReason.CONNECT else AegisNetworkReason.TIMEOUT
    is InterruptedIOException -> AegisNetworkReason.TIMEOUT
    else -> AegisNetworkReason.IO
}

/** 换一条线路也不会好的失败：证书不受信任（中间人、抓包、系统时间错到证书有效期外）。 */
internal fun IOException.isPermanent(): Boolean =
    this is SSLPeerUnverifiedException || (this is SSLHandshakeException && cause is CertificateException)

/**
 * 重试节奏。只对「换个时机大概率就好」的失败生效，见 [AegisClient] 的 execute。
 *
 * 次数刻意很少：每次尝试本身已经带着 OkHttp 在多个地址之间的快速切换，
 * 三次都不通说明是真不通，再试只会让界面多转几十秒。
 */
class AegisRetryPolicy @JvmOverloads constructor(
    /** 总尝试次数（含第一次）。 */
    val maxAttempts: Int = 3,
    /** 第 n 次重试前等待 `baseDelayMillis * 3^(n-1)`，再加至多 50% 的随机抖动。 */
    val baseDelayMillis: Long = 300,
) {
    internal fun delayBefore(retry: Int): Long {
        var delay = baseDelayMillis
        repeat(retry - 1) { delay *= 3 }
        return delay + (Math.random() * delay / 2).toLong()
    }

    companion object {
        @JvmField
        val NONE = AegisRetryPolicy(maxAttempts = 1)
    }
}

/**
 * 多路解析：系统 DNS → DNS over HTTPS → 内置的源站地址。
 *
 * 部分用户「一直连不上账号服务」几乎都出在这一层：运营商 DNS 把 CDN 的 CNAME 域名拦成
 * 0.0.0.0 / 127.0.0.1，或者解析到的 CDN 节点在该地区不可达。三个来源的结果按顺序合并后交给
 * OkHttp，OkHttp 的 Happy Eyeballs 会在 250 ms 内依次尝试后续地址，因此「前面的地址不通」
 * 只多花几百毫秒，而不是直接失败。
 *
 * 换地址不换主机名：TLS 的 SNI 与证书校验仍然按原域名进行，源站地址只是另一条路，
 * 不会因此降低安全性。
 */
internal class AegisDns(
    private val system: Dns,
    /** 只对这些主机名追加 [fallbackAddresses]。 */
    private val primaryHosts: Set<String>,
    private val fallbackAddresses: List<InetAddress>,
    private val doh: List<Dns>,
    private val systemTimeoutMillis: Long = 5_000,
) : Dns {

    private class Cached(val addresses: List<InetAddress>, val expiresAt: Long)

    private val dohCache = ConcurrentHashMap<String, Cached>()

    override fun lookup(hostname: String): List<InetAddress> {
        val systemResult = runCatching { lookupSystem(hostname) }
        val usable = systemResult.getOrNull().orEmpty().filterNot(::isBlackhole)

        val merged = LinkedHashSet<InetAddress>(usable)
        if (usable.isEmpty()) merged += lookupDoh(hostname)
        if (hostname.lowercase() in primaryHosts) merged += fallbackAddresses

        if (merged.isNotEmpty()) return merged.toList()
        val failure = systemResult.exceptionOrNull()
        throw (failure as? UnknownHostException)
            ?: UnknownHostException("无法解析 $hostname").apply { failure?.let(::initCause) }
    }

    /**
     * 系统解析器在部分 ROM 与弱网下会卡十几秒才放弃，而 OkHttp 的连接超时不覆盖 DNS。
     * 放到线程里限时，超时就当它失败，交给 DoH 与源站地址。
     */
    private fun lookupSystem(hostname: String): List<InetAddress> {
        val future = resolver.submit<List<InetAddress>> { system.lookup(hostname) }
        return try {
            future.get(systemTimeoutMillis, TimeUnit.MILLISECONDS)
        } catch (_: TimeoutException) {
            future.cancel(true)
            throw UnknownHostException("系统 DNS 解析 $hostname 超时")
        } catch (error: ExecutionException) {
            throw error.cause ?: error
        }
    }

    private fun lookupDoh(hostname: String): List<InetAddress> {
        val now = System.currentTimeMillis()
        dohCache[hostname]?.takeIf { it.expiresAt > now }?.let { return it.addresses }
        for (resolver in doh) {
            val addresses = runCatching { resolver.lookup(hostname) }.getOrNull()
                ?.filterNot(::isBlackhole)
                .orEmpty()
            if (addresses.isNotEmpty()) {
                dohCache[hostname] = Cached(addresses, now + DOH_CACHE_MILLIS)
                return addresses
            }
        }
        return emptyList()
    }

    /**
     * 运营商「反诈」与家用路由器拦截域名时的典型回应。真实的公网服务不会解析到这些地址。
     * 198.18.0.0/15 不在其中：那是 Clash 一类代理的 fake-ip 段，对开着代理的用户是可用的。
     */
    private fun isBlackhole(address: InetAddress): Boolean =
        address.isAnyLocalAddress || address.isLoopbackAddress ||
            address.isLinkLocalAddress || address.isSiteLocalAddress

    companion object {
        private const val DOH_CACHE_MILLIS = 10 * 60 * 1000L

        private val resolver = Executors.newCachedThreadPool { task ->
            Thread(task, "aegis-dns").apply { isDaemon = true }
        }

        /**
         * 公共 DoH。按引导 IP 直连，不依赖系统 DNS 能不能解析出 DoH 服务器自己的域名。
         * 国内两家在前：海外的那家在大陆经常不通，只作最后一选。
         */
        fun publicResolvers(bootstrap: OkHttpClient): List<Dns> = listOf(
            doh(bootstrap, "https://dns.alidns.com/dns-query", "223.5.5.5", "223.6.6.6"),
            doh(bootstrap, "https://doh.pub/dns-query", "1.12.12.12", "120.53.53.53"),
            doh(bootstrap, "https://cloudflare-dns.com/dns-query", "1.1.1.1", "1.0.0.1"),
        )

        private fun doh(bootstrap: OkHttpClient, url: String, vararg ips: String): Dns =
            DnsOverHttps.Builder()
                .client(bootstrap)
                .url(url.toHttpUrl())
                .bootstrapDnsHosts(ips.map { InetAddress.getByName(it) })
                .includeIPv6(true)
                .build()
    }
}
