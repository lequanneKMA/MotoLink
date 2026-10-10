package com.motolink.core.net

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.prefsStore by preferencesDataStore(name = "prefs")

/** Small non-secret preferences. */
class LocalPrefs(context: Context) {
    private val ds = context.applicationContext.prefsStore
    private val kLocationConsent = intPreferencesKey("location_consent_version")
    private val kBackgroundAsked = intPreferencesKey("background_prompt_count")

    /** Version of the location disclosure the user accepted (0 = never). docs/design/11 §5.1 */
    val locationConsentVersion: Flow<Int> = ds.data.map { it[kLocationConsent] ?: 0 }

    suspend fun acceptLocationConsent(version: Int) {
        ds.edit { it[kLocationConsent] = version }
    }

    suspend fun withdrawLocationConsent() {
        ds.edit { it.remove(kLocationConsent) }
    }

    val backgroundPromptCount: Flow<Int> = ds.data.map { it[kBackgroundAsked] ?: 0 }

    suspend fun bumpBackgroundPrompt() {
        ds.edit { it[kBackgroundAsked] = (it[kBackgroundAsked] ?: 0) + 1 }
    }

    companion object {
        /** Bump when the disclosure text changes materially: users are asked again. */
        const val LOCATION_CONSENT_VERSION = 1
    }
}
