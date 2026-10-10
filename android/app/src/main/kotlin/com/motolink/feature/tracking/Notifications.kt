package com.motolink.feature.tracking

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import com.motolink.MainActivity
import com.motolink.R

object Notifications {
    const val CHANNEL_TRACKING = "tracking"
    const val CHANNEL_SYSTEM = "system"
    const val ID_TRACKING = 1001

    fun createChannels(ctx: Context) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL_TRACKING, ctx.getString(R.string.notif_channel_tracking), NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
            },
        )
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL_SYSTEM, ctx.getString(R.string.notif_channel_system), NotificationManager.IMPORTANCE_LOW),
        )
    }

    fun tracking(ctx: Context, paused: Boolean): Notification {
        val open = PendingIntent.getActivity(
            ctx, 0, Intent(ctx, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val toggle = if (paused) TrackingService.ACTION_RESUME else TrackingService.ACTION_PAUSE
        return NotificationCompat.Builder(ctx, CHANNEL_TRACKING)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(ctx.getString(R.string.notif_tracking_title))
            .setContentText(ctx.getString(if (paused) R.string.notif_paused_text else R.string.notif_tracking_text))
            .setContentIntent(open)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .addAction(0, ctx.getString(if (paused) R.string.action_resume else R.string.action_pause), TrackingService.pendingAction(ctx, toggle))
            .addAction(0, ctx.getString(R.string.action_end), TrackingService.pendingAction(ctx, TrackingService.ACTION_END))
            .build()
    }
}
