package dev.aegis.sdk

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import java.io.IOException
import java.util.concurrent.atomic.AtomicReference

/**
 * 版本管控（`/client-control/check` 与 `/client-control/watch`）：加密上报客户端身份，取回服务端签名的裁决。
 *
 * 与应用安全等级无关、不需要登录：standard 档、未登录的客户端同样加密上报，同样拿到 Ed25519 签名的裁决。
 * 规格见服务端 `docs/client-control.md`，这里逐字节对应 `internal/service/client_control_crypto.go`：
 *
 * ```
 * 请求：X25519（临时密钥 × 服务端 box 公钥）→ HKDF-SHA256 → XChaCha20-Poly1305
 *   AAD  = "aegis-client-control-v1\n{appKey}\n{keyId}\nrequest\n{timestamp}\n{nonce}"
 *   salt = SHA-256("aegis-client-control-v1:{appKey}:{keyId}")，info = AAD
 * 响应：key = SHA-256(requestKey ‖ "aegis-client-control-response-v1")
 *   AAD  = "aegis-client-control-v1\n{appKey}\n{keyId}\nresponse\n{requestNonce}\n{responseNonce}"
 * 裁决签名：Ed25519("aegis-client-control-verdict-v1\n" ‖ verdict 原始字节)
 * box 公钥：Ed25519("aegis-client-control-box-v1\n{appKey}\n{keyId}\n{publicKey}")
 * 密钥背书：Ed25519(上一把签名密钥, "aegis-client-control-endorse-v1\n{appKey}\n{keyId}\n{publicKey}")
 * ```
 *
 * 信任从一把签名公钥开始：[AegisControlTrustStore] 给出内置（或首次使用时记下）的公钥，
 * 之后服务端轮换出的新签名密钥沿背书链逐把验证后加入。/config 本身没有签名，
 * 因此 box 公钥、新签名密钥都必须验过签才用；验不过一律抛异常，调用方保留已有裁决。
 *
 * 裁决的 `challenge` 等于本次请求的 nonce，所以只有 [check] 刚拿到的裁决是「新鲜」的；
 * 从存储里恢复的裁决（[restore]）只能证明它确实出自服务端，不能用来解除停用。
 */
class AegisClientControlApi internal constructor(private val client: AegisClient) {

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }
    private val trustRef = AtomicReference<AegisControlTrustStore>(AegisControlTrustStore.inMemory())

    /** 设置信任存储（内置公钥 + 持久化已验证的密钥）。应在第一次 [check] 之前调用。 */
    fun trust(store: AegisControlTrustStore) = apply { trustRef.set(store) }

    /** /config 里的管控规格；服务端未提供（旧版本服务端）时为 null。 */
    @Throws(IOException::class)
    fun spec(forceRefresh: Boolean = false): AegisClientControlSpec? = client.config(forceRefresh).clientControl

    /**
     * 加密上报 [identity]，返回验签通过、challenge 与身份都对得上的新鲜裁决。
     *
     * 失败（网络、验签、解密）一律抛 [AegisException]；调用方应保留此前保存的裁决，不要据此放行或停用。
     */
    @Throws(IOException::class)
    fun check(identity: AegisClientIdentity): AegisVerifiedVerdict {
        var spec = spec() ?: throw AegisException.transport("服务端未提供版本管控通道")
        return try {
            checkWith(spec, identity)
        } catch (error: AegisException) {
            // 密钥刚轮换（box 公钥退役 40457、签名密钥换新导致本地验不过）时 /config 可能还是旧的：
            // 强制刷新后只重试一次。网络错误不重试。
            val stale = error.code == BOX_KEY_UNKNOWN || (error.code == 0 && error.kind == AegisException.Kind.TRANSPORT)
            if (!stale) throw error
            spec = spec(forceRefresh = true) ?: throw error
            checkWith(spec, identity)
        }
    }

    /**
     * 长轮询：策略版本与 [since] 不同时立即返回 changed=true，否则最多挂起 [waitSeconds] 秒（服务端上限 25）。
     * 不需要登录；已登录时带上令牌。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun watch(since: Long, waitSeconds: Int = 25): AegisClientControlWatch {
        val wait = waitSeconds.coerceIn(0, 25)
        val element = client.callWithReadTimeout(
            path = "/client-control/watch",
            query = mapOf("since" to since.toString(), "wait" to wait.toString()),
            readTimeoutSeconds = wait + 10L,
        )
        return client.decodeAs(element, AegisClientControlWatch.serializer())
    }

    /**
     * 恢复保存的裁决：用信任存储里的公钥重新验签（离线可用）。验不过返回 null。
     * 恢复出的裁决 [AegisVerifiedVerdict.fresh] 为 false。
     */
    fun restore(stored: String): AegisVerifiedVerdict? = runCatching {
        val envelope = json.decodeFromString(AegisSignedVerdict.serializer(), stored)
        val trusted = trustRef.get().trustedKeys()
        val publicKey = trusted[envelope.signingKeyId] ?: return null
        val raw = AegisCrypto.decodeBase64Url(envelope.verdict)
        if (!verifyEd25519(publicKey, VERDICT_LABEL.toByteArray(Charsets.UTF_8) + raw, envelope.signature)) return null
        val verdict = json.decodeFromString(AegisClientVerdict.serializer(), raw.toString(Charsets.UTF_8))
        if (verdict.appKey != client.appKey) return null
        AegisVerifiedVerdict(verdict, envelope, fresh = false)
    }.getOrNull()

    // ── 内部 ──────────────────────────────────────────────────────────

    private fun checkWith(spec: AegisClientControlSpec, identity: AegisClientIdentity): AegisVerifiedVerdict {
        val appKey = client.appKey
        val trusted = resolveTrustedKeys(spec)
        val box = spec.boxKey
        val boxSigner = trusted[box.signingKeyId]
            ?: throw AegisException.transport("box 公钥的签名密钥 ${box.signingKeyId} 不在信任链上")
        if (!verifyEd25519(boxSigner, boxMessage(appKey, box.keyId, box.publicKey), box.signature)) {
            throw AegisException.transport("box 公钥签名无效，拒绝上报")
        }

        val ephemeral = AegisCrypto.generateEphemeralKeyPair()
        val shared = AegisCrypto.sharedSecret(ephemeral.privateKey, AegisCrypto.decodeBase64Url(box.publicKey))
        val nonce = AegisCrypto.randomBytes(AegisCrypto.NONCE_SIZE)
        val nonceB64 = AegisCrypto.encodeBase64Url(nonce)
        val timestamp = client.clock.nowSeconds()
        val aad = joinLines(PROTOCOL, appKey, box.keyId, "request", timestamp.toString(), nonceB64)
        val salt = AegisCrypto.sha256("$PROTOCOL:$appKey:${box.keyId}".toByteArray(Charsets.UTF_8))
        val requestKey = AegisCrypto.deriveKey(shared, salt, aad)
        val plaintext = json.encodeToString(AegisClientIdentity.serializer(), identity).toByteArray(Charsets.UTF_8)
        val sealed = AegisCrypto.seal(requestKey, nonce, plaintext, aad)

        val element = client.call(
            method = "POST",
            path = "/client-control/check",
            body = mapOf(
                "keyId" to box.keyId,
                "clientKey" to AegisCrypto.encodeBase64Url(ephemeral.publicKey),
                "nonce" to nonceB64,
                "timestamp" to timestamp,
                "payload" to AegisCrypto.encodeBase64Url(sealed),
            ),
            optionalAuth = true,
        )
        val response = client.decodeAs(element, SealedResponse.serializer())
        val responseKey = AegisCrypto.sha256(requestKey + RESPONSE_INFO.toByteArray(Charsets.UTF_8))
        val opened = AegisCrypto.open(
            responseKey,
            AegisCrypto.decodeBase64Url(response.nonce),
            AegisCrypto.decodeBase64Url(response.payload),
            joinLines(PROTOCOL, appKey, box.keyId, "response", nonceB64, response.nonce),
        )
        val envelope = json.decodeFromString(AegisSignedVerdict.serializer(), opened.toString(Charsets.UTF_8))
        val signer = trusted[envelope.signingKeyId]
            ?: throw AegisException.transport("裁决签名密钥 ${envelope.signingKeyId} 不在信任链上")
        val raw = AegisCrypto.decodeBase64Url(envelope.verdict)
        if (!verifyEd25519(signer, VERDICT_LABEL.toByteArray(Charsets.UTF_8) + raw, envelope.signature)) {
            throw AegisException.transport("裁决签名无效")
        }
        val verdict = json.decodeFromString(AegisClientVerdict.serializer(), raw.toString(Charsets.UTF_8))
        if (verdict.appKey != appKey) throw AegisException.transport("裁决不属于本应用")
        if (verdict.challenge != nonceB64) throw AegisException.transport("裁决与本次请求不匹配")
        val subject = verdict.subject
        if (subject.packageName != identity.packageName || subject.versionCode != identity.versionCode ||
            subject.certSha256 != normalizeCert(identity.certSha256) || subject.deviceId != identity.deviceId
        ) {
            throw AegisException.transport("裁决绑定的客户端身份与本机不一致")
        }
        return AegisVerifiedVerdict(verdict, envelope, fresh = true)
    }

    /**
     * 算出此刻可信的签名公钥：信任存储里的，加上沿背书链验证通过的新密钥。
     * 信任存储为空且没有内置公钥时，首次使用即信任背书链的根（TOFU），并记下来。
     */
    private fun resolveTrustedKeys(spec: AegisClientControlSpec): Map<String, String> {
        val store = trustRef.get()
        val trusted = LinkedHashMap(store.trustedKeys())
        if (trusted.isEmpty()) {
            val root = spec.signingKeys.firstOrNull { it.endorsedBy.isEmpty() }
                ?: throw AegisException.transport("服务端没有给出签名密钥")
            trusted[root.keyId] = root.publicKey
        }
        var grew = true
        while (grew) {
            grew = false
            for (key in spec.signingKeys) {
                if (key.keyId in trusted || key.endorsedBy.isEmpty()) continue
                val endorser = trusted[key.endorsedBy] ?: continue
                val message = joinLines(ENDORSE_LABEL, client.appKey, key.keyId, key.publicKey)
                if (verifyEd25519(endorser, message, key.endorsement)) {
                    trusted[key.keyId] = key.publicKey
                    grew = true
                }
            }
        }
        if (trusted != store.trustedKeys()) store.saveTrustedKeys(trusted)
        return trusted
    }

    private fun boxMessage(appKey: String, keyId: String, publicKey: String): ByteArray =
        joinLines(BOX_LABEL, appKey, keyId, publicKey)

    private fun joinLines(vararg parts: String): ByteArray =
        parts.joinToString("\n").toByteArray(Charsets.UTF_8)

    private fun verifyEd25519(publicKeyB64: String, message: ByteArray, signatureB64: String): Boolean = runCatching {
        val signer = Ed25519Signer()
        signer.init(false, Ed25519PublicKeyParameters(AegisCrypto.decodeBase64Url(publicKeyB64), 0))
        signer.update(message, 0, message.size)
        signer.verifySignature(AegisCrypto.decodeBase64Url(signatureB64))
    }.getOrDefault(false)

    @Serializable
    private data class SealedResponse(val keyId: String = "", val nonce: String = "", val payload: String = "")

    companion object {
        const val PROTOCOL = "aegis-client-control-v1"

        /** 网关闸门拒绝：停用。 */
        const val CODE_BLOCKED = 40329

        /** 网关闸门拒绝：强制更新。 */
        const val CODE_FORCE_UPDATE = 40326

        /** box 密钥不存在（已退役过久），刷新 /config 后重试。 */
        private const val BOX_KEY_UNKNOWN = 40457
        private const val RESPONSE_INFO = "aegis-client-control-response-v1"
        private const val VERDICT_LABEL = "aegis-client-control-verdict-v1\n"
        private const val BOX_LABEL = "aegis-client-control-box-v1"
        private const val ENDORSE_LABEL = "aegis-client-control-endorse-v1"

        /** 与服务端一致：去掉冒号、空格、连字符，转小写。 */
        @JvmStatic
        fun normalizeCert(value: String): String =
            value.trim().lowercase().replace(":", "").replace(" ", "").replace("-", "")
    }
}

/**
 * 管控签名公钥的信任存储。[trustedKeys] 是 keyId → base64url 公钥。
 *
 * 推荐内置一把公钥（从控制台「密钥」页复制）：[pinned] 给出它，持久化的部分只追加背书链上的新密钥。
 * 不内置时首次使用即信任服务端的根密钥（TOFU）——此后被替换的 /config 无法再伪造裁决。
 */
interface AegisControlTrustStore {
    fun trustedKeys(): Map<String, String>
    fun saveTrustedKeys(keys: Map<String, String>)

    companion object {
        @JvmStatic
        @JvmOverloads
        fun inMemory(pinned: Map<String, String> = emptyMap()): AegisControlTrustStore = object : AegisControlTrustStore {
            private val keys = AtomicReference<Map<String, String>>(pinned)
            override fun trustedKeys() = keys.get()
            override fun saveTrustedKeys(keys: Map<String, String>) = this.keys.set(pinned + keys)
        }
    }
}

/** 加密上报的明文。证书写法不限（带冒号、大写皆可），服务端统一归一化。 */
@Serializable
data class AegisClientIdentity(
    val packageName: String,
    val versionName: String = "",
    val versionCode: Long = 0,
    val platform: String = "android",
    /** 签名证书 SHA-256，64 位十六进制。 */
    val certSha256: String = "",
    /** 安装来源（安装器包名），未知为空。 */
    val installer: String = "",
    val deviceId: String = "",
    /** 系统 API 级别（Android `SDK_INT`）。 */
    val osVersion: Int = 0,
    val model: String = "",
    val brand: String = "",
    val abi: String = "",
    /** 本机当前持有的裁决对应的策略版本，仅用于统计。 */
    val policyVersion: Long = 0,
)

/** 服务端签名的裁决明文。签名覆盖的是它的原始字节，见 [AegisSignedVerdict]。 */
@Serializable
data class AegisClientVerdict(
    val v: Int = 1,
    val appKey: String = "",
    val policyVersion: Long = 0,
    val issuedAt: Long = 0,
    val expiresAt: Long = 0,
    val challenge: String = "",
    val subject: AegisVerdictSubject = AegisVerdictSubject(),
    val enabled: Boolean = false,
    /** allow / warn / force_update / block */
    val action: String = ACTION_ALLOW,
    val reason: String = "",
    val ruleId: Long = 0,
    val title: String = "",
    val message: String = "",
    val actionUrl: String = "",
    val actionLabel: String = "",
    val recheckSeconds: Int = 600,
    val watchEnabled: Boolean = true,
    /** 放行裁决过期且连不上服务端时：allow 继续放行，block 停用。 */
    val offlinePolicy: String = "allow",
) {
    /** 停用与强制更新都让客户端不可用。 */
    val blocks: Boolean get() = action == ACTION_BLOCK || action == ACTION_FORCE_UPDATE

    companion object {
        const val ACTION_ALLOW = "allow"
        const val ACTION_WARN = "warn"
        const val ACTION_FORCE_UPDATE = "force_update"
        const val ACTION_BLOCK = "block"
    }
}

@Serializable
data class AegisVerdictSubject(
    val packageName: String = "",
    val versionCode: Long = 0,
    val certSha256: String = "",
    val deviceId: String = "",
)

/** 签名裁决的信封：[verdict] 是裁决 JSON 原始字节的 base64url，保存时原样保存整个信封。 */
@Serializable
data class AegisSignedVerdict(
    val verdict: String = "",
    val signature: String = "",
    val signingKeyId: String = "",
)

/** 验签通过的裁决。[fresh] 表示刚由 [AegisClientControlApi.check] 取得（challenge 对得上本次请求）。 */
class AegisVerifiedVerdict internal constructor(
    val verdict: AegisClientVerdict,
    val envelope: AegisSignedVerdict,
    val fresh: Boolean,
) {
    /** 用于持久化的文本，交给 [AegisClientControlApi.restore] 恢复。 */
    fun encode(): String = Json.encodeToString(AegisSignedVerdict.serializer(), envelope)
}

/** `/config` 下发的管控规格。 */
@Serializable
data class AegisClientControlSpec(
    val protocol: String = "",
    val enabled: Boolean = false,
    val policyVersion: Long = 0,
    val recheckSeconds: Int = 600,
    val watchEnabled: Boolean = true,
    val signingKeyId: String = "",
    val signingKeys: List<AegisControlSigningKey> = emptyList(),
    val boxKey: AegisControlBoxKey = AegisControlBoxKey(),
    val checkPath: String = "",
    val watchPath: String = "",
)

@Serializable
data class AegisControlSigningKey(
    val keyId: String = "",
    val publicKey: String = "",
    val status: String = "",
    val endorsedBy: String = "",
    val endorsement: String = "",
)

@Serializable
data class AegisControlBoxKey(
    val keyId: String = "",
    val publicKey: String = "",
    val signingKeyId: String = "",
    val signature: String = "",
)

/** `GET /client-control/watch` 的结果。 */
@Serializable
data class AegisClientControlWatch(
    val policyVersion: Long = 0,
    /** 为 true 时立即重新 [AegisClientControlApi.check]。 */
    val changed: Boolean = false,
    /** 为 false 时服务端关闭了长轮询，改用定期复查，[retryAfter] 秒后再来。 */
    val watchEnabled: Boolean = true,
    val retryAfter: Int = 0,
)
