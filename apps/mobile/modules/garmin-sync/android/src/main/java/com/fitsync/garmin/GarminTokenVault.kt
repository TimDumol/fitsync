package com.fitsync.garmin

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import org.json.JSONObject
import java.io.File
import java.nio.charset.StandardCharsets
import java.security.KeyStore
import java.util.UUID
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

internal class GarminTokenVault(private val context: Context) {
  private val root = File(context.noBackupFilesDir, "garmin-vault")
  private val alias = "fitsync.garmin.aes-gcm.v1"

  init {
    root.mkdirs()
    root.setReadable(false, false)
    root.setWritable(false, false)
    root.setExecutable(false, false)
    root.setReadable(true, true)
    root.setWritable(true, true)
    root.setExecutable(true, true)
  }

  fun has(athleteID: String): Boolean = fileFor(athleteID).isFile

  fun read(athleteID: String): String? {
    val file = fileFor(athleteID)
    return try {
      if (!file.isFile) return null
      val envelope = JSONObject(file.readText(Charsets.UTF_8))
      if (envelope.optInt("schema_version") != 1 || envelope.optString("athlete_id") != athleteID) throw IllegalArgumentException()
      val nonce = android.util.Base64.decode(envelope.getString("nonce"), android.util.Base64.NO_WRAP)
      val ciphertext = android.util.Base64.decode(envelope.getString("ciphertext"), android.util.Base64.NO_WRAP)
      val cipher = Cipher.getInstance("AES/GCM/NoPadding")
      cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, nonce))
      cipher.updateAAD(athleteID.toByteArray(StandardCharsets.UTF_8))
      cipher.doFinal(ciphertext).toString(StandardCharsets.UTF_8).also {
        if (it.isBlank() || !JSONObject(it).keys().hasNext()) throw IllegalArgumentException()
      }
    } catch (_: Exception) {
      file.delete()
      null
    }
  }

  fun write(athleteID: String, tokenJson: String) {
    requireValidAthlete(athleteID)
    val parsed = JSONObject(tokenJson)
    if (!parsed.keys().hasNext()) throw IllegalArgumentException("empty token")
    val cipher = Cipher.getInstance("AES/GCM/NoPadding")
    cipher.init(Cipher.ENCRYPT_MODE, key())
    cipher.updateAAD(athleteID.toByteArray(StandardCharsets.UTF_8))
    val ciphertext = cipher.doFinal(tokenJson.toByteArray(StandardCharsets.UTF_8))
    val envelope = JSONObject()
      .put("schema_version", 1)
      .put("athlete_id", athleteID)
      .put("nonce", android.util.Base64.encodeToString(cipher.iv, android.util.Base64.NO_WRAP))
      .put("ciphertext", android.util.Base64.encodeToString(ciphertext, android.util.Base64.NO_WRAP))
    val target = fileFor(athleteID)
    val temporary = File(root, ".${target.name}.${UUID.randomUUID()}.tmp")
    temporary.writeText(envelope.toString(), Charsets.UTF_8)
    temporary.setReadable(false, false)
    temporary.setWritable(false, false)
    temporary.setReadable(true, true)
    temporary.setWritable(true, true)
    if (!temporary.renameTo(target)) {
      temporary.delete()
      throw IllegalStateException("token replacement failed")
    }
  }

  fun delete(athleteID: String) {
    fileFor(athleteID).delete()
  }

  private fun fileFor(athleteID: String): File {
    requireValidAthlete(athleteID)
    return File(root, "$athleteID.json")
  }

  private fun requireValidAthlete(athleteID: String) {
    UUID.fromString(athleteID)
  }

  private fun key(): SecretKey {
    val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
    val existing = store.getKey(alias, null)
    if (existing is SecretKey) return existing
    val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
    generator.init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
      .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
      .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
      .setUserAuthenticationRequired(false)
      .build())
    return generator.generateKey()
  }
}
