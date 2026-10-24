package dev.aegis.sdk

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

/**
 * 多线路：故障线路自动切走并立即重发、响应地址改写、手动模式不切换、测速择优。
 * 线路 A 是一个没人监听的端口（连接被拒），线路 B 是本机 HttpServer。
 */
class AegisLinesTest {

    private lateinit var server: HttpServer
    private val hits = AtomicInteger()
    private val deadPort = ServerSocket(0).use { it.localPort }

    private val live get() = "http://127.0.0.1:${server.address.port}"
    private val dead get() = "http://127.0.0.1:$deadPort"

    @BeforeTest
    fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.executor = Executors.newFixedThreadPool(8)
        server.createContext("/") { exchange ->
            hits.incrementAndGet()
            val path = exchange.requestURI.path
            when {
                path == "/healthz" -> exchange.reply(200, """{"code":200,"data":{"status":"healthy"}}""")
                path.endsWith("/config") -> exchange.reply(
                    200,
                    """{"code":200,"data":{"security":{"level":"standard"},"serverTime":{"unix":${System.currentTimeMillis() / 1000}}}}""",
                )
                // 服务端按自己的配置拼地址：总是指向线路 A 的源。
                path.endsWith("/banner") -> exchange.reply(
                    200,
                    """{"code":200,"data":{"image":"$dead/api/storage/proxy/x.png","nested":[{"socket":"${dead.replace("http://", "ws://")}/api/ws"}],"other":"https://cdn.example.com/a.png","token":"abc$dead"}}""",
                )
                path.endsWith("/order") -> exchange.reply(200, """{"code":200,"data":"ok"}""")
                else -> exchange.reply(404, "")
            }
        }
        server.start()
    }

    @AfterTest
    fun stop() = server.stop(0)

    private fun selector(mode: AegisLineMode = AegisLineMode.AUTO, pinned: String? = null) = AegisLineSelector(
        lines = listOf(AegisLine("a", "A", dead), AegisLine("b", "B", live)),
        mode = mode,
        pinnedId = pinned,
    )

    private fun client(lines: AegisLineSelector) = AegisClient.builder(dead, "k")
        .dnsOverHttps(false)
        .retryPolicy(AegisRetryPolicy(maxAttempts = 2, baseDelayMillis = 10))
        .lines(lines)
        .build()

    @Test
    fun deadLineIsAbandonedAndTheRequestResentOnTheNextOne() {
        val lines = selector()
        val client = client(lines)
        client.config()
        assertEquals("b", lines.active().id)
        val switch = assertNotNull(lines.state.lastSwitch)
        assertEquals("a", switch.fromId)
        assertEquals(false, lines.state.healthOf("a").available)
        assertTrue(lines.state.healthOf("a").cooldownUntil > System.currentTimeMillis())
    }

    @Test
    fun writesMoveToTheNextLineWhenTheyNeverLeftTheDevice() {
        val lines = selector()
        val data = client(lines).also { it.config() }.call("POST", "/order")
        assertEquals("ok", data.jsonPrimitive.content)
    }

    @Test
    fun urlsInResponsesFollowTheActiveLine() {
        val lines = selector()
        val client = client(lines)
        client.config()
        val data = client.call("GET", "/banner").jsonObject
        assertEquals("$live/api/storage/proxy/x.png", data["image"]!!.jsonPrimitive.content)
        val socket = data["nested"]!!.toString()
        assertTrue(socket.contains(live.replace("http://", "ws://") + "/api/ws"), socket)
        assertEquals("https://cdn.example.com/a.png", data["other"]!!.jsonPrimitive.content)
        // 只改开头：嵌在别的字符串中间的不动。
        assertEquals("abc$dead", data["token"]!!.jsonPrimitive.content)
    }

    @Test
    fun manualModeStaysOnThePinnedLine() {
        val lines = selector(AegisLineMode.MANUAL, pinned = "a")
        val failure = assertFailsWith<AegisException> { client(lines).config() }
        assertEquals(AegisException.Kind.NETWORK, failure.kind)
        assertEquals("a", lines.active().id)
        assertEquals(0, hits.get())
    }

    @Test
    fun probingPicksAReachableLine() {
        val lines = selector()
        client(lines)
        val state = lines.probeAll()
        assertEquals("b", state.activeId)
        assertEquals(true, state.healthOf("b").available)
        assertTrue(state.healthOf("b").latencyMillis >= 0)
        assertEquals(false, state.healthOf("a").available)
        assertTrue(state.probing.isEmpty())
    }

    @Test
    fun rebaseKeepsPathAndQuery() {
        val lines = selector().apply { pin("b") }
        val url = "$dead/api/v1/apps/k/me?x=1".toHttpUrl()
        assertEquals("$live/api/v1/apps/k/me?x=1", lines.rebase(url).toString())
        assertEquals("https://other.example.com/a", lines.rewrite("https://other.example.com/a"))
    }

    @Test
    fun recoveredLinesAreNotSwitchedBackForSmallGains() {
        val lines = AegisLineSelector(
            lines = listOf(AegisLine("a", "A", "https://a.example.com"), AegisLine("b", "B", "https://b.example.com")),
            restoredActiveId = "b",
            restoredHealth = mapOf("a" to AegisLineHealth(latencyMillis = 90, available = true), "b" to AegisLineHealth(latencyMillis = 100, available = true)),
        )
        lines.reportSuccess(lines.active())
        assertEquals("b", lines.active().id)
    }

    private fun HttpExchange.reply(status: Int, body: String) {
        val bytes = body.toByteArray()
        responseHeaders.add("Content-Type", "application/json")
        sendResponseHeaders(status, if (bytes.isEmpty()) -1 else bytes.size.toLong())
        responseBody.use { it.write(bytes) }
    }
}
