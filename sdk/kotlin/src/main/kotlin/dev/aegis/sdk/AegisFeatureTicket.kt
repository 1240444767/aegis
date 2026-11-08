package dev.aegis.sdk

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import java.io.IOException

/**
 * 权益凭证（`POST /vip/feature-ticket`）：服务端签名的「此人此设备此刻拥有某项权益」，离线也能验。
 *
 * 逐字节对应服务端 `internal/service/feature_ticket.go`：
 *
 * ```
 * 签名：Ed25519(版本管控签名密钥, "aegis-feature-ticket-v1\n" ‖ ticket 原始字节)
 * 背书：Ed25519(上一把签名密钥, "aegis-client-control-endorse-v1\n{appKey}\n{keyId}\n{publicKey}")
 * ```
 *
 * 验签只从调用方给的**内置**公钥出发，沿信封自带的背书链走到签名密钥（[verify]）；
 * 不读任何本地记下的密钥 —— 本地存储改得动，打进安装包的公钥改不动。
 */
class AegisFeatureTicketApi internal constructor(private val client: AegisClient) {

    /**
     * 为 [feature] 申请凭证，[digest] 一并签进去（例如配置的摘要，base64url，最多 128 字符）。
     * 返回的信封已验过签、challenge 对得上本次请求。没有该权益时抛 [AegisException]，code 为 [CODE_NOT_GRANTED]。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun issue(feature: String, pinnedKeys: Map<String, String>, digest: String = ""): AegisVerifiedFeatureTicket {
        val nonce = AegisCrypto.encodeBase64Url(AegisCrypto.randomBytes(24))
        val element = client.call(
            method = "POST",
            path = "/vip/feature-ticket",
            body = mapOf("feature" to feature, "digest" to digest, "nonce" to nonce),
            requireAuth = true,
        )
        val envelope = client.decodeAs(element, AegisFeatureTicketEnvelope.serializer())
        val verified = verify(envelope, client.appKey, pinnedKeys)
            ?: throw AegisException.transport("权益凭证签名无效")
        val ticket = verified.ticket
        if (ticket.challenge != nonce) throw AegisException.transport("权益凭证与本次请求不匹配")
        if (!ticket.feature.equals(feature.trim(), ignoreCase = true) || ticket.digest != digest) {
            throw AegisException.transport("权益凭证内容与请求不一致")
        }
        return verified
    }

    companion object {
        /** 服务端拒绝签发：当前账号没有该权益。客户端应删掉旧凭证。 */
        const val CODE_NOT_GRANTED = 40230

        private const val TICKET_LABEL = "aegis-feature-ticket-v1\n"
        private const val ENDORSE_LABEL = "aegis-client-control-endorse-v1"
        private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }

        /** 解析保存的信封文本并验签；格式不对或验不过返回 null。 */
        @JvmStatic
        fun restore(stored: String, appKey: String, pinnedKeys: Map<String, String>): AegisVerifiedFeatureTicket? =
            runCatching { json.decodeFromString(AegisFeatureTicketEnvelope.serializer(), stored) }.getOrNull()
                ?.let { verify(it, appKey, pinnedKeys) }

        /**
         * 只信 [pinnedKeys]（keyId → base64url 公钥），沿 [AegisFeatureTicketEnvelope.keys] 的背书链扩展，
         * 再用签名密钥验凭证。appKey 不符、签名不对都返回 null。是否过期、是否属于本机由调用方判断。
         */
        @JvmStatic
        fun verify(envelope: AegisFeatureTicketEnvelope, appKey: String, pinnedKeys: Map<String, String>): AegisVerifiedFeatureTicket? {
            if (pinnedKeys.isEmpty()) return null
            val trusted = LinkedHashMap(pinnedKeys)
            var grew = true
            while (grew) {
                grew = false
                for (key in envelope.keys) {
                    if (key.keyId in trusted || key.endorsedBy.isEmpty()) continue
                    val endorser = trusted[key.endorsedBy] ?: continue
                    val message = listOf(ENDORSE_LABEL, appKey, key.keyId, key.publicKey).joinToString("\n")
                    if (verifyEd25519(endorser, message.toByteArray(Charsets.UTF_8), key.endorsement)) {
                        trusted[key.keyId] = key.publicKey
                        grew = true
                    }
                }
            }
            val signer = trusted[envelope.signingKeyId] ?: return null
            val raw = runCatching { AegisCrypto.decodeBase64Url(envelope.ticket) }.getOrNull() ?: return null
            if (!verifyEd25519(signer, TICKET_LABEL.toByteArray(Charsets.UTF_8) + raw, envelope.signature)) return null
            val ticket = runCatching {
                json.decodeFromString(AegisFeatureTicket.serializer(), raw.toString(Charsets.UTF_8))
            }.getOrNull() ?: return null
            if (ticket.v != 1 || ticket.appKey != appKey) return null
            return AegisVerifiedFeatureTicket(ticket, envelope)
        }

        private fun verifyEd25519(publicKeyB64: String, message: ByteArray, signatureB64: String): Boolean = runCatching {
            val signer = Ed25519Signer()
            signer.init(false, Ed25519PublicKeyParameters(AegisCrypto.decodeBase64Url(publicKeyB64), 0))
            signer.update(message, 0, message.size)
            signer.verifySignature(AegisCrypto.decodeBase64Url(signatureB64))
        }.getOrDefault(false)
    }
}

/** 被签名的凭证明文。时间都是 Unix 秒。 */
@Serializable
data class AegisFeatureTicket(
    val v: Int = 0,
    val appKey: String = "",
    val feature: String = "",
    /** 对外用户 ID（与 /me 的 userId 一致）。 */
    val userId: Long = 0,
    val deviceId: String = "",
    /** 签发时是永久会员。 */
    val lifetime: Boolean = false,
    val digest: String = "",
    val challenge: String = "",
    val issuedAt: Long = 0,
    val expiresAt: Long = 0,
) {
    fun validAt(nowSeconds: Long): Boolean = nowSeconds in (issuedAt - TICKET_CLOCK_SKEW_SECONDS) until expiresAt
}

/** 允许本机时钟比服务端慢这么多。 */
private const val TICKET_CLOCK_SKEW_SECONDS = 600L

/** 信封：[ticket] 是凭证 JSON 原始字节的 base64url。保存时原样保存整个信封。 */
@Serializable
data class AegisFeatureTicketEnvelope(
    val ticket: String = "",
    val signature: String = "",
    val signingKeyId: String = "",
    val keys: List<AegisControlSigningKey> = emptyList(),
)

/** 验签通过的凭证。 */
class AegisVerifiedFeatureTicket internal constructor(
    val ticket: AegisFeatureTicket,
    val envelope: AegisFeatureTicketEnvelope,
) {
    fun encode(): String = Json.encodeToString(AegisFeatureTicketEnvelope.serializer(), envelope)
}
