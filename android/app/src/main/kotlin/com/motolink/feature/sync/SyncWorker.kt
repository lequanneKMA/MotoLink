package com.motolink.feature.sync

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.motolink.MotoLinkApp
import java.util.concurrent.TimeUnit

/**
 * Safety net for everything the realtime socket did not deliver (docs/design/12 §6): unacked GPS points, queued SOS,
 * and owed pause/resume/end calls. It runs when the network is back, periodically, and after a recording ends.
 */
class SyncWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val c = (applicationContext as MotoLinkApp).container
        if (c.tokens.current() == null) return Result.success()
        var allDone = true
        // SOS first: it is the only time-critical item.
        if (c.sos.flushViaRest() > 0) allDone = false
        for (sid in c.db.points().sessionsWithPending()) {
            val s = c.db.sessions().get(sid) ?: continue
            if (s.serverIssued && !c.uploader.drain(sid)) allDone = false
        }
        if (!c.sessionSync.pushAll()) allDone = false
        return if (allDone) Result.success() else Result.retry()
    }

    companion object {
        private const val ONCE = "motolink-sync-once"
        private const val PERIODIC = "motolink-sync-periodic"
        private val online = Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()

        /** Runs as soon as there is a network. Safe to call often. */
        fun kick(ctx: Context) {
            val req = OneTimeWorkRequestBuilder<SyncWorker>().setConstraints(online)
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS).build()
            WorkManager.getInstance(ctx).enqueueUniqueWork(ONCE, ExistingWorkPolicy.KEEP, req)
        }

        fun schedulePeriodic(ctx: Context) {
            val req = PeriodicWorkRequestBuilder<SyncWorker>(15, TimeUnit.MINUTES).setConstraints(online).build()
            WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.KEEP, req)
        }
    }
}
