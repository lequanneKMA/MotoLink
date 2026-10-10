package com.motolink.feature.sync

import com.motolink.core.db.AppDatabase
import com.motolink.core.net.ApiFailure
import com.motolink.core.net.ApiResult
import com.motolink.core.net.ApiService
import com.motolink.core.net.apiCall

/**
 * Pushes local pause/resume/end of server-issued sessions to the server. The local status is the user's intent;
 * `server_status` records what the server confirmed, so anything owed is retried until it sticks.
 */
class SessionSync(private val api: ApiService, private val db: AppDatabase, private val uploader: PointUploader) {

    /** @return true when the server is now in sync for [sessionId] (or there is nothing to sync). */
    suspend fun push(sessionId: String): Boolean {
        val s = db.sessions().get(sessionId) ?: return true
        if (!s.serverIssued || s.serverStatus == s.status) return true
        // Points first: once the session is ended on the server it refuses them.
        if (s.status == "ended") uploader.drain(sessionId)
        val r = when (s.status) {
            "paused" -> apiCall { api.pauseSession(sessionId) }
            "active" -> apiCall { api.resumeSession(sessionId) }
            else -> apiCall { api.endSession(sessionId) }
        }
        val done = when (r) {
            is ApiResult.Ok -> true
            is ApiResult.Err -> {
                val f = r.failure
                // 404/409: the server already moved on (trip ended, timeout, other device): nothing more to do.
                f is ApiFailure.Server && (f.status == 404 || f.status == 409)
            }
        }
        if (done) db.sessions().setServerStatus(sessionId, s.status)
        return done
    }

    suspend fun pushAll(): Boolean {
        var all = true
        for (s in db.sessions().needingServerSync()) if (!push(s.sessionId)) all = false
        return all
    }
}
