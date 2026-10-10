// The Google Services plugin (FCM config) is only put on the classpath when the developer added their own
// app/google-services.json; without it the app builds and runs, with push disabled (BuildConfig.HAS_FCM = false).
buildscript {
    repositories { google(); mavenCentral() }
    dependencies {
        if (file("app/google-services.json").exists()) classpath(libs.google.services.plugin)
    }
}

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.ksp) apply false
}
