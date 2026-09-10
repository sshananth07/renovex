package com.renovex.capture

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import com.renovex.capture.ui.RenovexNavHost

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val app = application as RenovexCaptureApp
        setContent {
            RenovexCaptureTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    RenovexNavHost(app.authRepository, app.networkModule)
                }
            }
        }
    }
}

@Composable
fun RenovexCaptureTheme(content: @Composable () -> Unit) {
    MaterialTheme(content = content)
}
