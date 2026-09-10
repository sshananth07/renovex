package com.renovex.capture

import android.app.Application
import com.renovex.capture.auth.AuthRepository
import com.renovex.capture.network.NetworkModule

class RenovexCaptureApp : Application() {
    lateinit var networkModule: NetworkModule
        private set
    lateinit var authRepository: AuthRepository
        private set

    override fun onCreate() {
        super.onCreate()
        networkModule = NetworkModule(this, BuildConfig.API_BASE_URL)
        authRepository = AuthRepository(this, networkModule)
    }
}
