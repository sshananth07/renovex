package com.renovex.capture.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.renovex.capture.network.SpatialApi
import com.renovex.capture.network.SpatialSpaceStateDto

/**
 * Design spec/Task 4 UX acceptance: Space -> Scan Room / AR Review /
 * Concepts. This screen establishes the three-mode entry point; the modes
 * themselves (Task 5 Scan Mode, Task 9 AR Review, Task 19 Concept AR) are
 * implemented in later tasks and are NOT collapsed into one generic screen
 * here even as placeholders — each button is understood as leading to a
 * behaviorally distinct experience (design spec §43.1).
 */
@Composable
fun SpaceDetailScreen(
    projectId: String,
    spaceId: String,
    spaceName: String,
    spatialApi: SpatialApi,
    onScanRoom: () -> Unit,
    pendingReviewSessionId: String? = null,
    onContinueReview: (String) -> Unit = {},
) {
    var spaceState by remember { mutableStateOf<SpatialSpaceStateDto?>(null) }

    LaunchedEffect(spaceId) {
        val response = runCatching { spatialApi.getSpaceState(projectId, spaceId) }.getOrNull()
        spaceState = response?.takeIf { it.isSuccessful }?.body()
    }

    Column(modifier = Modifier.fillMaxSize().padding(24.dp)) {
        Text(spaceName)

        val hasCurrentRoom = spaceState?.currentRoomVersionId?.isNotEmpty() == true
        Text(
            if (hasCurrentRoom) "Current room scan available" else "No spatial scan yet",
            modifier = Modifier.padding(top = 8.dp, bottom = 24.dp),
        )

        Button(onClick = onScanRoom, modifier = Modifier.fillMaxWidth()) {
            Text("Scan Room")
        }
        if (pendingReviewSessionId != null) {
            Button(
                onClick = { onContinueReview(pendingReviewSessionId) },
                modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
            ) {
                Text("Continue Room Review")
            }
        }
        Button(
            onClick = { /* Task 9: Existing-Space AR Review */ },
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
            enabled = hasCurrentRoom,
        ) {
            Text("AR Review")
        }
        Button(
            onClick = { /* Task 19: AI Concept AR */ },
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
            enabled = hasCurrentRoom,
        ) {
            Text("Concepts")
        }
    }
}
