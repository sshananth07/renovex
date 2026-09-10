package com.renovex.capture.ui.screens

import android.Manifest
import android.content.pm.PackageManager
import android.util.Log
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.google.ar.core.Config
import com.google.ar.core.Plane
import com.google.ar.core.Pose
import com.google.ar.core.Session
import com.google.ar.core.TrackingFailureReason
import com.google.ar.core.TrackingState
import com.renovex.capture.capture.ScanSessionState
import com.renovex.capture.capture.TrackingQuality
import com.renovex.capture.capture.RoomProposalStore
import com.renovex.capture.capture.arcore.ArCorePlaneObservationSource
import com.renovex.capture.capture.arcore.ArCorePlaneRegistry
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.ScannerParameters
import com.renovex.capture.ui.screens.scan.ScanModeHud
import com.renovex.capture.ui.screens.scan.ScanReconstructionController
import com.renovex.capture.ui.screens.scan.ScanReconstructionState
import com.renovex.capture.ui.screens.scan.RoomProposalReadinessTracker
import io.github.sceneview.ar.ARScene

/** Live AR scan backed by canonical plane observations and stateful room reconstruction. */
@Composable
fun ScanModeScreen(
    spaceId: String,
    spaceName: String,
    scanSessionId: String,
    proposalStore: RoomProposalStore,
    onReviewProposal: (String) -> Unit,
) {
    val context = LocalContext.current
    var hasCameraPermission by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) ==
                PackageManager.PERMISSION_GRANTED,
        )
    }
    val permissionLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        hasCameraPermission = granted
    }
    if (!hasCameraPermission) {
        CameraPermissionRequest(onRequest = { permissionLauncher.launch(Manifest.permission.CAMERA) })
        return
    }

    val planeRegistry = remember { ArCorePlaneRegistry() }
    val observationSource = remember { ArCorePlaneObservationSource(planeRegistry) }
    val scannerParameters = remember { ScannerParameters() }
    val reconstructionController = remember { ScanReconstructionController(scannerParameters) }
    val readinessTracker = remember { RoomProposalReadinessTracker(scannerParameters.proposalReadiness) }
    var sessionState by remember { mutableStateOf(ScanSessionState()) }
    var reconstruction by remember { mutableStateOf<ScanReconstructionState?>(null) }
    var lastCameraOnlyUpdateAt by remember { mutableStateOf(0L) }
    var lastDiagnosticLogAt by remember { mutableStateOf(0L) }

    val storedStage = proposalStore.get(scanSessionId)?.stage
    LaunchedEffect(storedStage) {
        if (storedStage == com.renovex.capture.capture.RoomAcquisitionStage.SCANNING &&
            proposalStore.consumeScanningResume(scanSessionId)
        ) {
            readinessTracker.resumeScanning()
            reconstruction = null
        }
    }

    Box(modifier = Modifier.fillMaxSize()) {
        ARScene(
            modifier = Modifier.fillMaxSize(),
            sessionConfiguration = { session: Session, config: Config ->
                config.depthMode = if (session.isDepthModeSupported(Config.DepthMode.AUTOMATIC)) {
                    Config.DepthMode.AUTOMATIC
                } else {
                    Config.DepthMode.DISABLED
                }
                config.planeFindingMode = Config.PlaneFindingMode.HORIZONTAL_AND_VERTICAL
            },
            onSessionUpdated = { _: Session, frame ->
                if (frame.camera.trackingState == TrackingState.TRACKING &&
                    sessionState.trackingQuality != TrackingQuality.GOOD
                ) {
                    sessionState = sessionState.copy(trackingQuality = TrackingQuality.GOOD)
                }

                val now = System.currentTimeMillis()
                val updatedPlanes = frame.getUpdatedTrackables(Plane::class.java)
                val observations = observationSource.observationsFrom(updatedPlanes, now)
                val shouldRefreshCameraOnly = now - lastCameraOnlyUpdateAt >= CAMERA_GUIDANCE_INTERVAL_MILLIS
                if (readinessTracker.latched == null && (observations.isNotEmpty() || shouldRefreshCameraOnly)) {
                    if (shouldRefreshCameraOnly) lastCameraOnlyUpdateAt = now
                    val cameraPose = frame.camera.pose
                    val next = reconstructionController.update(
                        planeObservations = observations,
                        cameraPosition = PointXZ(cameraPose.tx().toDouble(), cameraPose.tz().toDouble()),
                        cameraHeadingRadians = cameraHeadingRadians(cameraPose),
                    )
                    reconstruction = next
                    sessionState = sessionState.copy(coveragePercent = next.progress.coveragePercent)
                    readinessTracker.observe(
                        shellResult = next.shellResult,
                        progress = next.progress,
                        candidates = next.candidates,
                        trackingUsable = sessionState.trackingQuality == TrackingQuality.GOOD,
                    )
                    if (now - lastDiagnosticLogAt >= DIAGNOSTIC_LOG_INTERVAL_MILLIS) {
                        lastDiagnosticLogAt = now
                        Log.d("RenovexScan", "reconstruction=${next.diagnostics}")
                        Log.d("RenovexLineage", next.diagnostics.compactLineageSummary())
                    }
                }
            },
            onTrackingFailureChanged = { reason: TrackingFailureReason? ->
                val quality = when (reason) {
                    null -> TrackingQuality.NOT_STARTED
                    TrackingFailureReason.EXCESSIVE_MOTION -> TrackingQuality.LIMITED_EXCESSIVE_MOTION
                    TrackingFailureReason.INSUFFICIENT_LIGHT -> TrackingQuality.LIMITED_INSUFFICIENT_LIGHT
                    TrackingFailureReason.INSUFFICIENT_FEATURES -> TrackingQuality.LIMITED_INSUFFICIENT_FEATURES
                    else -> TrackingQuality.LIMITED_OTHER
                }
                sessionState = sessionState.copy(trackingQuality = quality)
            },
        )

        ScanModeHud(
            spaceName = spaceName,
            state = sessionState,
            reconstruction = reconstruction,
            onReviewProposal = readinessTracker.latched?.let { ready ->
                    {
                        proposalStore.createFromShell(
                            scanSessionId = scanSessionId,
                            spaceId = spaceId,
                            shellResult = RoomShellBuildResult.Complete(ready.shell),
                            ceilingHeight = sessionState.draft.ceilingHeight,
                            candidates = ready.candidates,
                            simplificationConfig = scannerParameters.shellSimplification,
                        )?.let { onReviewProposal(scanSessionId) }
                    }
                },
        )
    }
}

@Composable
private fun CameraPermissionRequest(onRequest: () -> Unit) {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Camera access is needed to scan the room")
            Button(onClick = onRequest, modifier = Modifier.padding(top = 16.dp)) {
                Text("Grant camera permission")
            }
        }
    }
}

/** ARCore camera faces down pose -Z; convert that to atan2(z, x). */
private fun cameraHeadingRadians(cameraPose: Pose): Double {
    val outward = cameraPose.zAxis
    return kotlin.math.atan2(-outward[2].toDouble(), -outward[0].toDouble())
}

private const val CAMERA_GUIDANCE_INTERVAL_MILLIS = 400L
private const val DIAGNOSTIC_LOG_INTERVAL_MILLIS = 1_000L
