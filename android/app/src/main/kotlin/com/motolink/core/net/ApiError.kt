package com.motolink.core.net

import kotlinx.serialization.json.Json
import retrofit2.HttpException
import java.io.IOException

/** What the UI needs to know about a failed call. */
sealed interface ApiFailure {
    data object Network : ApiFailure
    data class Server(val status: Int, val code: String, val fields: List<FieldErrorDto> = emptyList(), val retryAfterS: Int? = null) : ApiFailure
    data class Unknown(val message: String?) : ApiFailure
}

private val problemJson = Json { ignoreUnknownKeys = true; explicitNulls = false }

fun Throwable.toApiFailure(): ApiFailure = when (this) {
    is IOException -> ApiFailure.Network
    is HttpException -> {
        val body = response()?.errorBody()?.string()
        val p = body?.let { runCatching { problemJson.decodeFromString(ProblemDto.serializer(), it) }.getOrNull() }
        ApiFailure.Server(
            status = code(),
            code = p?.code ?: "http_${code()}",
            fields = p?.errors.orEmpty(),
            retryAfterS = response()?.headers()?.get("Retry-After")?.toIntOrNull(),
        )
    }
    else -> ApiFailure.Unknown(message)
}
