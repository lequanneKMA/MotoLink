package com.motolink.feature.notifications

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.motolink.MainActivity
import com.motolink.R
import com.motolink.feature.realtime.SosIncoming

/** Notification channels and builders for trip/SOS alerts (the recording notification lives in feature.tracking). */
object AppNotifications {
    const val CHANNEL_SOS = "sos"
    const val CHANNEL_TRIP = "trip"
    const val EXTRA_TRIP_ID = "trip_id"
    const val EXTRA_OPEN_SOS = "open_sos"

    fun createChannels(ctx: Context) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL_SOS, "Cảnh báo SOS", NotificationManager.IMPORTANCE_HIGH).apply {
                description = "Khi một thành viên trong chuyến đi cần giúp đỡ"
                enableVibration(true)
                setBypassDnd(false)
            },
        )
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL_TRIP, "Chuyến đi", NotificationManager.IMPORTANCE_DEFAULT).apply {
                description = "Chuyến đi bắt đầu/kết thúc, thành viên mới"
            },
        )
    }

    private fun canPost(ctx: Context) =
        Build.VERSION.SDK_INT < 33 || ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    private fun openTrip(ctx: Context, tripId: String, sos: Boolean, requestCode: Int): PendingIntent {
        val i = Intent(ctx, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_NEW_TASK)
            .putExtra(EXTRA_TRIP_ID, tripId).putExtra(EXTRA_OPEN_SOS, sos)
        return PendingIntent.getActivity(ctx, requestCode, i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    fun kindLabel(kind: String) = when (kind) {
        "accident" -> "tai nạn"
        "breakdown" -> "hỏng xe"
        "lost" -> "bị lạc"
        "medical" -> "cần y tế"
        else -> "cần giúp đỡ"
    }

    /** Shown for a SOS received over the socket (no coordinates in the notification text). */
    fun showSos(ctx: Context, s: SosIncoming) {
        if (!canPost(ctx)) return
        val who = s.sos.senderName ?: "Một thành viên"
        val n = NotificationCompat.Builder(ctx, CHANNEL_SOS)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("SOS: $who ${kindLabel(s.sos.kind)}")
            .setContentText(s.sos.message ?: "Mở ứng dụng để xem vị trí")
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_ALARM)
            .setAutoCancel(true)
            .setContentIntent(openTrip(ctx, s.tripId, true, s.sos.sosId.hashCode()))
            .build()
        runCatching { NotificationManagerCompat.from(ctx).notify(s.sos.sosId.hashCode(), n) }
    }

    /** Shown for a push message; [data] holds identifiers only (docs/design/09 §5.2). */
    fun showPush(ctx: Context, title: String?, body: String?, data: Map<String, String>) {
        if (!canPost(ctx)) return
        val type = data["type"].orEmpty()
        val sos = type == "sos"
        val tripId = data["trip_id"].orEmpty()
        val n = NotificationCompat.Builder(ctx, if (sos) CHANNEL_SOS else CHANNEL_TRIP)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title ?: "MotoLink")
            .setContentText(body.orEmpty())
            .setPriority(if (sos) NotificationCompat.PRIORITY_HIGH else NotificationCompat.PRIORITY_DEFAULT)
            .setAutoCancel(true)
            .setContentIntent(openTrip(ctx, tripId, sos, (data["notification_id"] ?: type).hashCode()))
            .build()
        runCatching { NotificationManagerCompat.from(ctx).notify((data["notification_id"] ?: "$type$tripId").hashCode(), n) }
    }
}
