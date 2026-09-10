package com.renovex.capture

import androidx.activity.ComponentActivity

/** Explicit debug-only Compose host for physical-device UI tests on MIUI. */
class RoomReviewTestHostActivity : ComponentActivity() {
    override fun onResume() {
        super.onResume()
        current = this
    }

    override fun onDestroy() {
        if (current === this) current = null
        super.onDestroy()
    }

    companion object {
        @Volatile
        var current: RoomReviewTestHostActivity? = null
            private set
    }
}
