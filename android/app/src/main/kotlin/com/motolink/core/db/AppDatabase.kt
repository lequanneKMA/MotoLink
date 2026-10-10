package com.motolink.core.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(
    entities = [LocalSessionEntity::class, TrackPointEntity::class, TripEntity::class, PendingSosEntity::class],
    version = 2,
    exportSchema = true,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun sessions(): SessionDao
    abstract fun points(): PointDao
    abstract fun trips(): TripDao
    abstract fun sos(): SosDao

    companion object {
        fun create(context: Context): AppDatabase =
            Room.databaseBuilder(context.applicationContext, AppDatabase::class.java, "motolink.db")
                // Before the first release there is no migration path to maintain; afterwards this line must go
                // and real migrations be written (schemas are exported to app/schemas).
                .fallbackToDestructiveMigration(dropAllTables = true)
                .build()
    }
}
