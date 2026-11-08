package dev.aegis.sdk

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.ConnectionPool
import okhttp3.Dns
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.io.InterruptedIOException
import java.net.ConnectException
import java.net.InetAddress
import java.net.NoRouteToHostException
import java.net.PortUnreachableException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.util.Collections
import java.util.WeakHashMap
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLHandshakeException
import javax.net.ssl.SSLPeerUnverifiedException

/**
 * 一条通往同一个服务端的线路。
 *
 * 线路之间只换「连到哪里」：[baseUrl] 的协议、主机与端口，以及可选的 [addresses]（跳过 DNS，
 * 直接连这些 IP，TLS 仍按 [baseUrl] 的域名校验证书）。路径不在线路里：签名覆盖请求路径，
 * 换线路改了路径，服务端就验不过签名。所以 [baseUrl] 只认源（`https://host[:port]`），写了路径会被丢掉。
 *
 * 典型的三条：CDN 域名、另一家 CDN 回源的域名、同一个域名直连源站 IP。
 */
class AegisLine @JvmOverloads constructor(
    /** 稳定标识，持久化与手动选择用。 */
    val id: String,
    /** 给人看的名字。 */
    val name: String,
    baseUrl: String,
    /** 直连地址（IP 字面量）。非空时这条线路不解析域名，只连这些地址。 */
    addresses: List<String> = emptyList(),
    /** 内置线路不能被删除，界面据此区分。 */
    val builtIn: Boolean = true,
) {
    /** 规范化后的源，例如 `https://aegis.example.com`；非默认端口时带端口。 */
    val baseUrl: String

    val addresses: List<String> = addresses.map(String::trim).filter { IP_LITERAL.matches(it) }

    internal val url: HttpUrl

    val host: String get() = url.host

    init {
        val parsed = baseUrl.trim().trimEnd('/').toHttpUrl()
        url = parsed.newBuilder().encodedPath("/").query(null).fragment(null).build()
        this.baseUrl = url.toString().trimEnd('/')
    }

    internal val inetAddresses: List<InetAddress> by lazy {
        this.addresses.mapNotNull { runCatching { InetAddress.getByName(it) }.getOrNull() }
    }

    /** 两条线路落到同一个连接目标上：同源且直连地址相同。 */
    internal val routeKey: String get() = baseUrl + "|" + addresses.joinToString(",")

    internal fun sameOrigin(other: HttpUrl): Boolean =
        other.host.equals(url.host, ignoreCase = true) && other.port == url.port &&
            other.isHttps == url.isHttps

    override fun equals(other: Any?): Boolean =
        other is AegisLine && other.id == id && other.routeKey == routeKey && other.name == name && other.builtIn == builtIn

    override fun hashCode(): Int = id.hashCode() * 31 + routeKey.hashCode()
    override fun toString(): String = "AegisLine($id, $baseUrl${if (addresses.isEmpty()) "" else " → $addresses"})"

    internal companion object {
        val IP_LITERAL = Regex("""^(\d{1,3}(\.\d{1,3}){3}|[0-9a-fA-F:]*:[0-9a-fA-F:.]*)$""")
    }
}

/** 线路选择方式。 */
enum class AegisLineMode {
    /** 按连通性与延迟自动选择；当前线路出现连接级失败时立即换到下一条。 */
    AUTO,

    /** 固定使用用户指定的线路，不自动切换。 */
    MANUAL,
}

/**
 * 一条线路的健康状况。
 *
 * [latencyMillis] 是测速时复用连接的一次往返（接近 RTT）；[connectMillis] 是第一次请求的耗时，
 * 含解析、建连与 TLS 握手。两者都是 -1 表示还没测过或这次没测通。
 */
data class AegisLineHealth(
    val latencyMillis: Long = -1,
    val connectMillis: Long = -1,
    /** 最近一次的结论：true 通、false 不通、null 未知。 */
    val available: Boolean? = null,
    /** 连续失败次数，成功一次清零。 */
    val failures: Int = 0,
    /** 冷却到这个时刻（毫秒）之前，自动模式不选它。 */
    val cooldownUntil: Long = 0,
    val checkedAt: Long = 0,
    /** 最近一次失败的说明与 [AegisNetworkReason]。 */
    val lastError: String = "",
    val reason: String = AegisNetworkReason.NONE,
)

/** 一次自动切换的记录。 */
data class AegisLineSwitch(
    val fromId: String,
    val toId: String,
    /** 触发原因：连接失败的说明，或「测速择优」。 */
    val cause: String,
    val at: Long,
)

/** 选择器的一份快照。不可变，可以直接交给界面。 */
data class AegisLineState(
    val lines: List<AegisLine>,
    val mode: AegisLineMode,
    /** 手动模式下用户指定的线路。 */
    val pinnedId: String?,
    /** 此刻请求实际走的线路。 */
    val activeId: String,
    val health: Map<String, AegisLineHealth>,
    /** 正在测速的线路。 */
    val probing: Set<String> = emptySet(),
    val lastSwitch: AegisLineSwitch? = null,
) {
    val active: AegisLine get() = lines.first { it.id == activeId }
    fun healthOf(id: String): AegisLineHealth = health[id] ?: AegisLineHealth()
}

/**
 * 多线路选择器：决定每个请求走哪条线路，并把响应里指向其他线路的地址换成当前线路。
 *
 * 请求一律按第一条线路（[canonical]）的地址构造，再由 [interceptor] 在发出前换成当前线路的源。
 * 这样 SDK 与调用方存下来的地址都只有一种写法，切换线路不必重建任何对象。
 *
 * 自动模式下的切换有两个来源：
 *   - 被动：当前线路出现连接级失败（解析不到、连不上、握手失败、超时、CDN 回 502/504 之类），
 *     记一次失败、进入冷却（15 秒起，指数增长，最长 5 分钟），立刻换到最好的另一条。
 *     [AegisClient] 发现线路变了，会在新线路上立即重发（仍受「请求能否重发」的约束）。
 *   - 主动：[probeAll] 测速后，明显更快的线路（快 30% 且至少 60 ms）取代当前线路；差不多时不换，
 *     避免来回跳。
 *
 * 线程安全。状态变化通过 [addListener] 通知，回调在触发变化的线程上执行。
 */
class AegisLineSelector @JvmOverloads constructor(
    lines: List<AegisLine>,
    mode: AegisLineMode = AegisLineMode.AUTO,
    pinnedId: String? = null,
    /** 上次进程记住的当前线路（自动模式下沿用，省掉启动时的一次试错）。 */
    restoredActiveId: String? = null,
    /** 上次进程记住的延迟，只用于排序；冷却与失败计数不跨进程。 */
    restoredHealth: Map<String, AegisLineHealth> = emptyMap(),
) {
    @Volatile
    var state: AegisLineState
        private set

    private val listeners = CopyOnWriteArrayList<(AegisLineState) -> Unit>()
    private val pools: MutableSet<ConnectionPool> = Collections.synchronizedSet(Collections.newSetFromMap(WeakHashMap()))

    @Volatile
    private var prober: AegisLineProber? = null

    init {
        require(lines.isNotEmpty()) { "至少需要一条线路" }
        require(lines.map { it.id }.toSet().size == lines.size) { "线路 id 重复" }
        val health = restoredHealth.filterKeys { id -> lines.any { it.id == id } }
            .mapValues { (_, h) -> AegisLineHealth(latencyMillis = h.latencyMillis, connectMillis = h.connectMillis, available = h.available, checkedAt = h.checkedAt) }
        val pinned = pinnedId?.takeIf { id -> lines.any { it.id == id } }
        val effectiveMode = if (mode == AegisLineMode.MANUAL && pinned == null) AegisLineMode.AUTO else mode
        val active = when (effectiveMode) {
            AegisLineMode.MANUAL -> pinned!!
            AegisLineMode.AUTO -> restoredActiveId?.takeIf { id -> lines.any { it.id == id } } ?: lines.first().id
        }
        state = AegisLineState(lines.toList(), effectiveMode, pinned, active, health)
    }

    /** 请求按它的地址构造。 */
    val canonical: AegisLine get() = state.lines.first()

    fun active(): AegisLine = state.active

    val lines: List<AegisLine> get() = state.lines

    fun addListener(listener: (AegisLineState) -> Unit) {
        listeners += listener
    }

    fun removeListener(listener: (AegisLineState) -> Unit) {
        listeners -= listener
    }

    // ── 配置 ─────────────────────────────────────────────────────────────

    /**
     * 换一组线路（例如用户增删了自定义线路）。第一条仍须是构造请求用的那条，保留下来的线路沿用健康记录；
     * 当前线路被删掉时重新选择。
     */
    fun setLines(lines: List<AegisLine>) {
        require(lines.isNotEmpty()) { "至少需要一条线路" }
        require(lines.map { it.id }.toSet().size == lines.size) { "线路 id 重复" }
        update { current ->
            val health = current.health.filterKeys { id -> lines.any { it.id == id } }
            val pinned = current.pinnedId?.takeIf { id -> lines.any { it.id == id } }
            val mode = if (current.mode == AegisLineMode.MANUAL && pinned == null) AegisLineMode.AUTO else current.mode
            val draft = current.copy(lines = lines.toList(), health = health, pinnedId = pinned, mode = mode)
            val activeId = when {
                mode == AegisLineMode.MANUAL -> pinned!!
                lines.any { it.id == current.activeId && it.routeKey == current.active.routeKey } -> current.activeId
                else -> best(draft, System.currentTimeMillis(), exclude = null).id
            }
            draft.copy(activeId = activeId)
        }
    }

    /** 切到自动模式，立刻按现有健康记录重新选一次。 */
    fun useAuto() = update { current ->
        val draft = current.copy(mode = AegisLineMode.AUTO, pinnedId = null)
        val now = System.currentTimeMillis()
        val keep = current.healthOf(current.activeId).cooldownUntil <= now
        draft.copy(activeId = if (keep) current.activeId else best(draft, now, exclude = null).id)
    }

    /** 固定使用 [lineId]。 */
    fun pin(lineId: String) = update { current ->
        require(current.lines.any { it.id == lineId }) { "没有这条线路：$lineId" }
        current.copy(mode = AegisLineMode.MANUAL, pinnedId = lineId, activeId = lineId)
    }

    // ── 运行时反馈 ───────────────────────────────────────────────────────

    /** 这条线路上刚完成了一次往返。 */
    fun reportSuccess(line: AegisLine) {
        val health = state.health[line.id]
        if (health != null && health.failures == 0 && health.available == true) return
        update { current ->
            if (current.lines.none { it.id == line.id }) return@update current
            val previous = current.healthOf(line.id)
            current.copy(health = current.health + (line.id to previous.copy(available = true, failures = 0, cooldownUntil = 0, lastError = "", reason = AegisNetworkReason.NONE)))
        }
    }

    /**
     * 这条线路上出现了连接级失败。自动模式下它若是当前线路，立刻换到最好的另一条。
     * [httpStatus] 非 0 表示是 CDN / 网关回的错误状态码，而不是 I/O 异常。
     */
    fun reportFailure(line: AegisLine, error: IOException?, httpStatus: Int = 0) {
        val now = System.currentTimeMillis()
        val message = when {
            error != null -> error.message ?: error.javaClass.simpleName
            else -> "HTTP $httpStatus"
        }
        val reason = error?.networkReason() ?: AegisNetworkReason.HTTP
        update { current ->
            if (current.lines.none { it.id == line.id }) return@update current
            val previous = current.healthOf(line.id)
            val failures = previous.failures + 1
            val cooldown = (BASE_COOLDOWN_MILLIS shl (failures - 1).coerceAtMost(5)).coerceAtMost(MAX_COOLDOWN_MILLIS)
            val health = previous.copy(available = false, failures = failures, cooldownUntil = now + cooldown, checkedAt = now, lastError = message, reason = reason)
            val draft = current.copy(health = current.health + (line.id to health))
            if (draft.mode != AegisLineMode.AUTO || draft.activeId != line.id || draft.lines.size < 2) return@update draft
            val next = best(draft, now, exclude = line.id)
            if (next.id == line.id) draft
            else draft.copy(activeId = next.id, lastSwitch = AegisLineSwitch(line.id, next.id, message, now))
        }
    }

    // ── 测速 ─────────────────────────────────────────────────────────────

    /** 能否测速：选择器要先交给一个 [AegisClient]，测速借用它的网络配置。 */
    val canProbe: Boolean get() = prober != null

    /**
     * 并行测每一条线路，阻塞到全部完成（单条最长约 10 秒）。测出来的结果逐条更新到 [state]，
     * 界面可以边测边显示。自动模式下测完按延迟择优。
     */
    fun probeAll(): AegisLineState {
        val prober = prober ?: return state
        val targets = state.lines
        update { it.copy(probing = it.probing + targets.map(AegisLine::id)) }
        val futures = targets.map { line -> line to probeExecutor.submit<AegisLineHealth> { prober.probe(line) } }
        futures.forEach { (line, future) ->
            val result = runCatching { future.get(PROBE_WAIT_SECONDS, TimeUnit.SECONDS) }
                .getOrElse { AegisLineHealth(available = false, checkedAt = System.currentTimeMillis(), lastError = "测速超时", reason = AegisNetworkReason.TIMEOUT) }
            record(line, result)
        }
        update { current -> if (current.mode == AegisLineMode.AUTO) reselect(current) else current }
        return state
    }

    /** 只测一条。 */
    fun probe(lineId: String): AegisLineHealth {
        val prober = prober ?: return state.healthOf(lineId)
        val line = state.lines.firstOrNull { it.id == lineId } ?: return AegisLineHealth()
        update { it.copy(probing = it.probing + lineId) }
        record(line, prober.probe(line))
        return state.healthOf(lineId)
    }

    private fun record(line: AegisLine, result: AegisLineHealth) = update { current ->
        val previous = current.healthOf(line.id)
        val merged = if (result.available == true) {
            result.copy(failures = 0, cooldownUntil = 0)
        } else {
            val failures = previous.failures + 1
            val cooldown = (BASE_COOLDOWN_MILLIS shl (failures - 1).coerceAtMost(5)).coerceAtMost(MAX_COOLDOWN_MILLIS)
            result.copy(failures = failures, cooldownUntil = result.checkedAt + cooldown)
        }
        var next = current.copy(health = current.health + (line.id to merged), probing = current.probing - line.id)
        // 当前线路测不通：不必等其余线路测完，先换。
        if (next.mode == AegisLineMode.AUTO && next.activeId == line.id && merged.available == false && next.lines.size > 1) {
            val best = best(next, result.checkedAt, exclude = line.id)
            if (best.id != line.id) next = next.copy(activeId = best.id, lastSwitch = AegisLineSwitch(line.id, best.id, merged.lastError, result.checkedAt))
        }
        next
    }

    /** 测速后的择优：当前线路不可用就换；可用时只有明显更快才换。 */
    private fun reselect(current: AegisLineState): AegisLineState {
        val now = System.currentTimeMillis()
        val best = best(current, now, exclude = null)
        if (best.id == current.activeId) return current
        val active = current.healthOf(current.activeId)
        val candidate = current.healthOf(best.id)
        val activeUsable = active.cooldownUntil <= now && active.available != false
        if (activeUsable) {
            val a = active.latencyMillis
            val b = candidate.latencyMillis
            if (a < 0 || b < 0) return current
            if (!(b < a * 7 / 10 && a - b >= SWITCH_MARGIN_MILLIS)) return current
        }
        val cause = if (activeUsable) "测速择优" else active.lastError.ifEmpty { "当前线路不可用" }
        return current.copy(activeId = best.id, lastSwitch = AegisLineSwitch(current.activeId, best.id, cause, now))
    }

    /**
     * 不在冷却中的线路里，延迟最低的那条；没测过的排在测通的后面，彼此按配置顺序。
     * 全在冷却中时选最先结束冷却的。
     */
    private fun best(current: AegisLineState, now: Long, exclude: String?): AegisLine {
        val pool = current.lines.filter { it.id != exclude }.ifEmpty { current.lines }
        val ready = pool.filter { current.healthOf(it.id).cooldownUntil <= now }
        if (ready.isEmpty()) return pool.minBy { current.healthOf(it.id).cooldownUntil }
        return ready.withIndex().minBy { (index, line) ->
            val health = current.healthOf(line.id)
            when {
                health.available != false && health.latencyMillis >= 0 -> health.latencyMillis
                health.available != false -> UNKNOWN_SCORE + index
                else -> FAILED_SCORE + index
            }
        }.value
    }

    private inline fun update(transform: (AegisLineState) -> AegisLineState) {
        val before: AegisLineState
        val after: AegisLineState
        synchronized(this) {
            before = state
            after = transform(before)
            if (after == before) return
            state = after
        }
        if (before.active.routeKey != after.active.routeKey) {
            // 同一个域名在直连与非直连之间切换时，连接池里的空闲连接仍指向旧的地址。
            synchronized(pools) { pools.toList() }.forEach { runCatching { it.evictAll() } }
        }
        listeners.forEach { listener -> runCatching { listener(after) } }
    }

    // ── 地址改写 ─────────────────────────────────────────────────────────

    /** [url] 指向任意一条线路时返回那条线路（同源的多条里取第一条），否则 null。 */
    fun lineOf(url: HttpUrl): AegisLine? = state.lines.firstOrNull { it.sameOrigin(url) }

    /** [host] 是否属于某条线路。 */
    fun isLineHost(host: String): Boolean = state.lines.any { it.host.equals(host, ignoreCase = true) }

    /** 把指向任意线路的 [url] 换到 [line]（默认当前线路）的源上；不是线路地址的原样返回。 */
    @JvmOverloads
    fun rebase(url: HttpUrl, line: AegisLine = active()): HttpUrl {
        if (lineOf(url) == null || line.sameOrigin(url)) return url
        return url.newBuilder().scheme(line.url.scheme).host(line.url.host).port(line.url.port).build()
    }

    /**
     * 字符串版本：以某条线路的源开头的地址（http/https，以及对应的 ws/wss）换成当前线路的源。
     * 只看开头：签名、令牌、base64 里不会以 `https://` 开头，不会被误改。
     */
    fun rewrite(url: String): String {
        if (url.length < 8) return url
        val target = active()
        val lower = url.lowercase()
        for (line in state.lines) {
            if (line.baseUrl.equals(target.baseUrl, ignoreCase = true)) continue
            for ((from, to) in originPairs(line, target)) {
                if (lower.startsWith(from) && (url.length == from.length || url[from.length] in "/?#")) {
                    return to + url.substring(from.length)
                }
            }
        }
        return url
    }

    private fun originPairs(from: AegisLine, to: AegisLine): List<Pair<String, String>> {
        val source = from.baseUrl.lowercase()
        val ws = if (source.startsWith("https://")) "wss://" + source.removePrefix("https://") else "ws://" + source.removePrefix("http://")
        val targetWs = if (to.baseUrl.startsWith("https://")) "wss://" + to.baseUrl.removePrefix("https://") else "ws://" + to.baseUrl.removePrefix("http://")
        return listOf(source to to.baseUrl, ws to targetWs)
    }

    /** JSON 版本：递归改写每个字符串值。没有要改的时返回原对象。 */
    fun rewrite(element: JsonElement): JsonElement = when (element) {
        is JsonObject -> {
            var changed = false
            val mapped = element.mapValues { (_, value) -> rewrite(value).also { if (it !== value) changed = true } }
            if (changed) JsonObject(mapped) else element
        }
        is JsonArray -> {
            var changed = false
            val mapped = element.map { value -> rewrite(value).also { if (it !== value) changed = true } }
            if (changed) JsonArray(mapped) else element
        }
        is JsonPrimitive -> if (element.isString) {
            val text = element.content
            val replaced = rewrite(text)
            if (replaced === text) element else JsonPrimitive(replaced)
        } else element
        else -> element
    }

    // ── OkHttp 接入 ──────────────────────────────────────────────────────

    /**
     * 把发往任意线路的请求改到当前线路（请求带了 [AegisLine] 标签时改到那条），并给请求打上标签，
     * 下游的发送层据此选连接方式。
     *
     * [reportOutcome] 为 true 时把连接级失败与成功反馈给选择器 —— 只在 SDK 自己的客户端上开；
     * 给图片加载等共用客户端装的那一份只做改写，不应让一张图的失败牵动账号请求的线路。
     */
    @JvmOverloads
    fun interceptor(reportOutcome: Boolean = false): Interceptor = Interceptor { chain ->
        val request = chain.request()
        if (lineOf(request.url) == null) return@Interceptor chain.proceed(request)
        val line = request.tag(AegisLine::class.java) ?: active()
        val routed = if (line.sameOrigin(request.url) && request.tag(AegisLine::class.java) === line) request
        else request.newBuilder().url(rebase(request.url, line)).tag(AegisLine::class.java, line).build()
        if (!reportOutcome) return@Interceptor chain.proceed(routed)

        val response = try {
            chain.proceed(routed)
        } catch (error: IOException) {
            if (!chain.call().isCanceled() && error.isLineFailure()) reportFailure(line, error)
            throw error
        }
        if (response.code in GATEWAY_FAILURES) reportFailure(line, null, response.code) else reportSuccess(line)
        response
    }

    /** 当前线路是直连线路时，它的域名直接给出直连地址；其余交给 [delegate]。 */
    // 写成对象表达式而不是 SAM 构造：上游钉的 OkHttp 4 里 Dns 还不是 fun interface。
    fun dns(delegate: Dns): Dns = object : Dns {
        override fun lookup(hostname: String): List<InetAddress> {
            val line = active()
            return if (line.addresses.isNotEmpty() && line.host.equals(hostname, ignoreCase = true)) {
                line.inetAddresses.ifEmpty { delegate.lookup(hostname) }
            } else {
                delegate.lookup(hostname)
            }
        }
    }

    internal fun attach(prober: AegisLineProber, pool: ConnectionPool) {
        if (this.prober == null) this.prober = prober
        pools += pool
    }

    private companion object {
        const val BASE_COOLDOWN_MILLIS = 15_000L
        const val MAX_COOLDOWN_MILLIS = 5 * 60_000L
        const val SWITCH_MARGIN_MILLIS = 60L
        const val UNKNOWN_SCORE = 5_000L
        const val FAILED_SCORE = 50_000L
        const val PROBE_WAIT_SECONDS = 15L

        /** CDN / 反向代理自己回的「上游不通」。业务错误走 200 + 信封，不会是这些。 */
        val GATEWAY_FAILURES = setOf(502, 503, 504, 520, 521, 522, 523, 524, 525, 526, 530)

        val probeExecutor = Executors.newCachedThreadPool { task ->
            Thread(task, "aegis-line-probe").apply { isDaemon = true }
        }
    }
}

/**
 * 记为线路故障的失败：请求没送达（解析、建连、握手、证书），或者等不到响应。
 * 证书错误也算：运营商把域名劫持到拦截页时回的正是一张对不上的证书，换条线路往往就好了。
 */
internal fun IOException.isLineFailure(): Boolean = when (this) {
    is UnknownHostException, is ConnectException, is NoRouteToHostException, is PortUnreachableException -> true
    is SSLHandshakeException, is SSLPeerUnverifiedException -> true
    is SocketTimeoutException -> true
    is InterruptedIOException -> message.orEmpty().contains("timeout", ignoreCase = true)
    else -> false
}

/**
 * 测速：对一条线路发两次 `GET /healthz`。第一次在全新的连接池上（含解析、建连、握手），
 * 第二次复用连接，记作延迟。走 OkHttp、不走发送层引擎，几条线路的数字才可比。
 */
internal class AegisLineProber(
    private val base: OkHttpClient,
    private val resolver: Dns,
    private val path: String = "/healthz",
) {
    private val json = Json { ignoreUnknownKeys = true }

    fun probe(line: AegisLine): AegisLineHealth {
        val pool = ConnectionPool(1, 10, TimeUnit.SECONDS)
        val client = base.newBuilder()
            .dns(if (line.addresses.isNotEmpty()) PinnedDns(line) else resolver)
            .connectionPool(pool)
            .retryOnConnectionFailure(false)
            .connectTimeout(6, TimeUnit.SECONDS)
            .readTimeout(6, TimeUnit.SECONDS)
            .callTimeout(10, TimeUnit.SECONDS)
            .build()
        val url = line.url.newBuilder().encodedPath(path).build()
        return try {
            val coldStart = System.nanoTime()
            once(client, url, line)
            val cold = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - coldStart)
            val warmStart = System.nanoTime()
            val warm = runCatching {
                once(client, url, line)
                TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - warmStart)
            }.getOrDefault(cold)
            AegisLineHealth(latencyMillis = warm, connectMillis = cold, available = true, checkedAt = System.currentTimeMillis())
        } catch (error: IOException) {
            AegisLineHealth(
                available = false,
                checkedAt = System.currentTimeMillis(),
                lastError = error.message ?: error.javaClass.simpleName,
                reason = if (error is HealthException) AegisNetworkReason.HTTP else error.networkReason(),
            )
        } finally {
            pool.evictAll()
        }
    }

    private fun once(client: OkHttpClient, url: HttpUrl, line: AegisLine) {
        val request = Request.Builder().url(url).header("Cache-Control", "no-cache")
            .tag(AegisLine::class.java, line).get().build()
        client.newCall(request).execute().use { response: Response ->
            val text = response.body?.string().orEmpty()
            val code = runCatching { json.parseToJsonElement(text).jsonObject["code"]?.jsonPrimitive?.intOrNull }.getOrNull()
            if (!response.isSuccessful || code != 200) {
                throw HealthException("HTTP ${response.code}：${if (code == null) "响应不是平台信封" else "健康检查未通过"}")
            }
        }
    }

    private class HealthException(message: String) : IOException(message)

    private class PinnedDns(private val line: AegisLine) : Dns {
        override fun lookup(hostname: String): List<InetAddress> =
            line.inetAddresses.ifEmpty { throw UnknownHostException("直连地址无效") }
    }
}
