package com.renovex.capture.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.renovex.capture.auth.AuthRepository
import com.renovex.capture.auth.AuthSessionState
import com.renovex.capture.capture.RoomProposalStore
import com.renovex.capture.network.NetworkModule
import com.renovex.capture.ui.screens.LoginScreen
import com.renovex.capture.ui.screens.ProjectListScreen
import com.renovex.capture.ui.screens.RoomReviewScreen
import com.renovex.capture.ui.screens.ScanModeScreen
import com.renovex.capture.ui.screens.SpaceDetailScreen
import com.renovex.capture.ui.screens.SpaceListScreen
import com.renovex.capture.ui.screens.SplashScreen
import kotlinx.coroutines.launch
import java.util.UUID

private object Routes {
    const val SPLASH = "splash"
    const val LOGIN = "login"
    const val PROJECTS = "projects"
    const val SPACES = "spaces/{projectId}/{projectName}"
    const val SPACE_DETAIL = "space/{projectId}/{spaceId}/{spaceName}"
    const val SCAN_MODE = "scan/{spaceId}/{spaceName}/{scanSessionId}"
    const val ROOM_REVIEW = "room-review/{scanSessionId}"

    fun spaces(projectId: String, projectName: String) = "spaces/$projectId/$projectName"
    fun spaceDetail(projectId: String, spaceId: String, spaceName: String) =
        "space/$projectId/$spaceId/$spaceName"
    fun scanMode(spaceId: String, spaceName: String, scanSessionId: String) =
        "scan/$spaceId/$spaceName/$scanSessionId"
    fun roomReview(scanSessionId: String) = "room-review/$scanSessionId"
}

/**
 * The app's top-level navigation shell (design spec/Task 4 UX acceptance):
 * Projects -> Project -> Spaces -> Space -> Scan Room / AR Review /
 * Concepts. Every route carries human-readable NAMES alongside IDs so
 * screen titles never present raw database IDs as primary navigation.
 */
@Composable
fun RenovexNavHost(authRepository: AuthRepository, networkModule: NetworkModule) {
    val navController = rememberNavController()
    var sessionState by remember { mutableStateOf<AuthSessionState>(AuthSessionState.Unknown) }
    val roomProposalStore = remember { RoomProposalStore() }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        sessionState = authRepository.restoreSession()
        navController.navigateAfterRestore(sessionState)
    }

    NavHost(navController = navController, startDestination = Routes.SPLASH) {
        composable(Routes.SPLASH) { SplashScreen() }

        composable(Routes.LOGIN) {
            LoginScreen(
                onLogin = { email, password ->
                    scope.launch {
                        val result = authRepository.login(email, password)
                        sessionState = result
                        if (result is AuthSessionState.LoggedIn) {
                            navController.navigate(Routes.PROJECTS) {
                                popUpTo(Routes.LOGIN) { inclusive = true }
                            }
                        }
                    }
                },
            )
        }

        composable(Routes.PROJECTS) {
            ProjectListScreen(
                projectSpaceApi = networkModule.projectSpaceApi,
                onProjectSelected = { id, name ->
                    navController.navigate(Routes.spaces(id, name))
                },
            )
        }

        composable(Routes.SPACES) { backStackEntry ->
            val projectId = backStackEntry.arguments?.getString("projectId").orEmpty()
            val projectName = backStackEntry.arguments?.getString("projectName").orEmpty()
            SpaceListScreen(
                projectId = projectId,
                projectName = projectName,
                projectSpaceApi = networkModule.projectSpaceApi,
                onSpaceSelected = { id, name ->
                    navController.navigate(Routes.spaceDetail(projectId, id, name))
                },
            )
        }

        composable(Routes.SPACE_DETAIL) { backStackEntry ->
            val projectId = backStackEntry.arguments?.getString("projectId").orEmpty()
            val spaceId = backStackEntry.arguments?.getString("spaceId").orEmpty()
            val spaceName = backStackEntry.arguments?.getString("spaceName").orEmpty()
            val pendingReviewSessionId = roomProposalStore.pendingForSpace(spaceId)?.scanSessionId
            SpaceDetailScreen(
                projectId = projectId,
                spaceId = spaceId,
                spaceName = spaceName,
                spatialApi = networkModule.spatialApi,
                onScanRoom = {
                    navController.navigate(Routes.scanMode(spaceId, spaceName, UUID.randomUUID().toString()))
                },
                pendingReviewSessionId = pendingReviewSessionId,
                onContinueReview = { navController.navigate(Routes.roomReview(it)) },
            )
        }

        composable(Routes.SCAN_MODE) { backStackEntry ->
            val spaceId = backStackEntry.arguments?.getString("spaceId").orEmpty()
            val spaceName = backStackEntry.arguments?.getString("spaceName").orEmpty()
            val scanSessionId = backStackEntry.arguments?.getString("scanSessionId").orEmpty()
            ScanModeScreen(
                spaceId = spaceId,
                spaceName = spaceName,
                scanSessionId = scanSessionId,
                proposalStore = roomProposalStore,
                onReviewProposal = { navController.navigate(Routes.roomReview(it)) },
            )
        }

        composable(Routes.ROOM_REVIEW) { backStackEntry ->
            val scanSessionId = backStackEntry.arguments?.getString("scanSessionId").orEmpty()
            RoomReviewScreen(
                scanSessionId = scanSessionId,
                proposalStore = roomProposalStore,
                onContinueScanning = { navController.popBackStack() },
                onConfirmed = {
                    navController.popBackStack(Routes.SPACE_DETAIL, inclusive = false)
                },
            )
        }
    }
}

private fun NavHostController.navigateAfterRestore(state: AuthSessionState) {
    val destination = if (state is AuthSessionState.LoggedIn) Routes.PROJECTS else Routes.LOGIN
    navigate(destination) { popUpTo(Routes.SPLASH) { inclusive = true } }
}
