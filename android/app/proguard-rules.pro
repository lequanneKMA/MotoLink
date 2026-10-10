# kotlinx.serialization: keep generated serializers
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.AnnotationsKt
-keepclassmembers class kotlinx.serialization.json.** { *** Companion; }
-keepclasseswithmembers class kotlinx.serialization.json.** { kotlinx.serialization.KSerializer serializer(...); }
-keep,includedescriptorclasses class com.motolink.**$$serializer { *; }
-keepclassmembers class com.motolink.** { *** Companion; }
-keepclasseswithmembers class com.motolink.** { kotlinx.serialization.KSerializer serializer(...); }
# Retrofit interfaces
-keepattributes Signature, Exceptions
-keep interface com.motolink.core.net.** { *; }
