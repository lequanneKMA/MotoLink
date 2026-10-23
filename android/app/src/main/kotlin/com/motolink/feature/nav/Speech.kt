package com.motolink.feature.nav

import android.content.Context
import android.speech.tts.TextToSpeech
import java.util.Locale

/**
 * Voice guidance through the phone's own text-to-speech engine in Vietnamese (works offline when the voice data is
 * installed). Without a Vietnamese voice the app stays silent and the banner is the only guidance.
 */
class Speech(context: Context) {
    @Volatile private var ready = false
    @Volatile var enabled = true
    private var tts: TextToSpeech? = null

    init {
        tts = TextToSpeech(context.applicationContext) { status ->
            if (status == TextToSpeech.SUCCESS) {
                val r = tts?.setLanguage(Locale.forLanguageTag("vi-VN"))
                ready = r != null && r != TextToSpeech.LANG_MISSING_DATA && r != TextToSpeech.LANG_NOT_SUPPORTED
            }
        }
    }

    fun say(text: String) {
        if (!enabled || !ready) return
        tts?.speak(text, TextToSpeech.QUEUE_FLUSH, null, "nav")
    }

    fun shutdown() {
        tts?.stop()
        tts?.shutdown()
        tts = null
    }
}
