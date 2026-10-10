package com.motolink.core.net

import kotlinx.coroutines.CancellationException

sealed interface ApiResult<out T> {
    data class Ok<T>(val value: T) : ApiResult<T>
    data class Err(val failure: ApiFailure) : ApiResult<Nothing>
}

/** Runs a network call and maps every failure to [ApiFailure]; cancellation is never swallowed. */
suspend fun <T> apiCall(block: suspend () -> T): ApiResult<T> = try {
    ApiResult.Ok(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Exception) {
    ApiResult.Err(e.toApiFailure())
}

inline fun <T> ApiResult<T>.onOk(f: (T) -> Unit): ApiResult<T> {
    if (this is ApiResult.Ok) f(value)
    return this
}

fun ApiFailure.userMessage(): String = when (this) {
    ApiFailure.Network -> "Không có kết nối mạng. Thử lại sau."
    is ApiFailure.Unknown -> "Có lỗi xảy ra. Thử lại sau."
    is ApiFailure.Server -> when (code) {
        "invalid_code", "not_found" -> "Không tìm thấy (mã sai hoặc đã hết hạn)."
        "expired" -> "Mã mời đã hết hạn."
        "trip_full" -> "Chuyến đi đã đủ người."
        "already_member" -> "Bạn đã ở trong chuyến đi này."
        "removed" -> "Bạn đã bị mời ra khỏi chuyến đi này."
        "consent_required" -> "Cần đồng ý chia sẻ vị trí trước."
        "trip_not_active" -> "Chuyến đi chưa bắt đầu hoặc đã kết thúc."
        "session_already_open" -> "Bạn đang ghi hành trình trong chuyến đi này."
        "rate_limited" -> "Thử quá nhiều lần. Đợi một lúc rồi thử lại."
        "forbidden" -> "Bạn không có quyền thực hiện việc này."
        else -> if (status >= 500) "Máy chủ đang bận. Thử lại sau." else "Yêu cầu không hợp lệ ($code)."
    }
}
