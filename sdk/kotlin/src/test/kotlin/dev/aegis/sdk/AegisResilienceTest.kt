package dev.aegis.sdk

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import okhttp3.Dns
import okhttp3.OkHttpClient
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.UnknownHostException
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * 请求管线的恢复行为：单飞刷新、网关错误页重发、非幂等请求不重发、DNS 被拦时走备用地址。
 * 全部对本机的 JDK HttpServer 跑（standard 档，免包装），不依赖外网。
 */
class AegisResilienceTest {

    private lateinit var server: HttpServer
    private val refreshes = AtomicInteger()
    private val flakyHits = AtomicInteger()
    private val postHits = AtomicInteger()

    private val port get() = server.address.port

    @BeforeTest
    fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.executor = Executors.newFixedThreadPool(16)
        server.createContext("/") { exchange ->
            val path = exchange.requestURI.path
            val auth = exchange.requestHeaders.getFirst("Authorization").orEmpty()
            when {
                path.endsWith("/config") -> exchange.reply(
                    200,
                    """{"code":200,"data":{"security":{"level":"standard"},"serverTime":{"unix":${System.currentTimeMillis() / 1000}}}}""",
                )
                path.endsWith("/auth/refresh") -> {
                    Thread.sleep(100)
                    val n = refreshes.incrementAndGet()
                    exchange.reply(200, """{"code":200,"data":{"accessToken":"new$n","refreshToken":"r$n"}}""")
                }
                path.endsWith("/me") ->
                    if (auth == "Bearer old") exchange.reply(200, """{"code":40100,"message":"expired"}""")
                    else exchange.reply(200, """{"code":200,"data":{"ok":true}}""")
                path.endsWith("/flaky") ->
                    if (flakyHits.incrementAndGet() < 3) exchange.reply(502, "<html>502 Bad Gateway</html>", "text/html")
                    else exchange.reply(200, """{"code":200,"data":"fine"}""")
                path.endsWith("/order") -> {
                    postHits.incrementAndGet()
                    exchange.reply(502, "<html>502</html>", "text/html")
                }
                else -> exchange.reply(404, "")
            }
        }
        server.start()
    }

    @AfterTest
    fun stop() = server.stop(0)

    private fun client(
        base: String = "http://127.0.0.1:$port",
        tokens: AegisTokenStore = AegisTokenStore.inMemory(),
        configure: AegisClient.Builder.() -> Unit = {},
    ): AegisClient = AegisClient.builder(base, "k")
        .tokenStore(tokens)
        .dnsOverHttps(false)
        .retryPolicy(AegisRetryPolicy(maxAttempts = 3, baseDelayMillis = 10))
        .apply(configure)
        .build()
        .also { it.config() }

    @Test
    fun concurrentExpiredTokensRefreshOnce() {
        val tokens = AegisTokenStore.inMemory().apply { save("old", "r0") }
        val client = client(tokens = tokens)
        val pool = Executors.newFixedThreadPool(12)
        val results = (1..12).map {
            pool.submit<Boolean> { runCatching { client.call("GET", "/me", requireAuth = true) }.isSuccess }
        }.map { it.get() }
        pool.shutdown()

        // 每个线程各刷一次的话，服务端会把第二次起判为刷新令牌复用并吊销整条会话链。
        assertEquals(1, refreshes.get())
        assertTrue(results.all { it })
        assertEquals("new1", tokens.accessToken())
    }

    @Test
    fun gatewayErrorPagesAreRetriedForReads() {
        val data = client().call("GET", "/flaky")
        assertEquals("\"fine\"", data.toString())
        assertEquals(3, flakyHits.get())
    }

    @Test
    fun writesAreNotResentAfterReachingTheServer() {
        val failure = assertFailsWith<AegisException> { client().call("POST", "/order") }
        assertEquals(AegisException.Kind.NETWORK, failure.kind)
        assertEquals(AegisNetworkReason.HTTP, failure.networkReason)
        assertEquals(502, failure.httpStatus)
        assertEquals(1, postHits.get())
    }

    @Test
    fun blockedDnsFallsBackToConfiguredAddress() {
        // 运营商 DNS 把域名拦成 0.0.0.0：应当丢弃这个结果，改连备用地址。
        val blackholed = OkHttpClient.Builder()
            .dns(dns { listOf(InetAddress.getByName("0.0.0.0")) })
            .build()
        val client = client(base = "http://aegis.test:$port") {
            httpClient(blackholed)
            fallbackAddresses(listOf("127.0.0.1"))
        }
        assertEquals("\"fine\"", run { flakyHits.set(2); client.call("GET", "/flaky").toString() })
    }

    @Test
    fun unresolvableHostReportsDns() {
        val failing = OkHttpClient.Builder().dns(dns { throw UnknownHostException(it) }).build()
        val failure = assertFailsWith<AegisException> {
            AegisClient.builder("http://aegis.test:$port", "k")
                .httpClient(failing)
                .dnsOverHttps(false)
                .retryPolicy(AegisRetryPolicy(maxAttempts = 2, baseDelayMillis = 1))
                .build()
                .config()
        }
        assertEquals(AegisNetworkReason.DNS, failure.networkReason)
    }

    /** OkHttp 4 的 Dns 不是 fun interface，上游 SDK 钉在 4.12，所以不用 SAM 写法。 */
    private fun dns(lookup: (String) -> List<InetAddress>): Dns = object : Dns {
        override fun lookup(hostname: String): List<InetAddress> = lookup(hostname)
    }

    private fun HttpExchange.reply(code: Int, body: String, type: String = "application/json") {
        val bytes = body.toByteArray()
        responseHeaders.add("Content-Type", type)
        sendResponseHeaders(code, if (bytes.isEmpty()) -1 else bytes.size.toLong())
        responseBody.use { it.write(bytes) }
    }
}
