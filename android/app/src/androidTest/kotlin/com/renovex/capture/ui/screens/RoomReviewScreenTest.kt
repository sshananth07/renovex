package com.renovex.capture.ui.screens

import android.util.Log
import android.os.SystemClock
import androidx.activity.compose.setContent
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import com.renovex.capture.capture.RoomProposalStore
import com.renovex.capture.RoomReviewTestHostActivity
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class RoomReviewScreenTest {
    @get:Rule
    val composeRule = createEmptyComposeRule()

    @Test
    fun review_enters_real_corner_edit_cancel_apply_and_confirms_applied_geometry() {
        // Break caught: corner rows and chevrons look editable but invoke a
        // no-op using the existing coordinate.
        val store = RoomProposalStore()
        store.createFromShell("scan", "space", RoomShellBuildResult.Complete(squareShell()))
        Log.i(TAG, "checkpoint store-ready")
        val host = awaitHostActivity()
        composeRule.runOnUiThread { host.setContent { RoomReviewScreen("scan", store) } }
        Log.i(TAG, "checkpoint content-set")

        composeRule.onNodeWithText("Ceiling height: Unconfirmed").assertExists()
        composeRule.onNodeWithText("Floor area: 12.00 m²").assertExists()
        Log.i(TAG, "checkpoint summary-found")
        composeRule.onNodeWithText("Corner 2", substring = true).performScrollTo().performClick()
        Log.i(TAG, "checkpoint corner-selected")
        composeRule.onNodeWithText("Edit corner 2").assertExists()
        composeRule.onNodeWithTag("corner-x-input").performTextReplacement("5.0")
        composeRule.onNodeWithTag("corner-z-input").performTextReplacement("0.0")
        composeRule.onNodeWithText("Update corner").performScrollTo().performClick()
        Log.i(TAG, "checkpoint corner-updated")
        composeRule.onNodeWithText("Floor area: 13.50 m²").assertExists()

        composeRule.onNodeWithText("Cancel changes").performScrollTo().performClick()
        Log.i(TAG, "checkpoint cancelled")
        composeRule.onNodeWithText("Floor area: 12.00 m²").assertExists()
        assertEquals(4.0, store.get("scan")!!.draft.corners[1].x, 0.0)

        composeRule.onNodeWithText("Corner 2", substring = true).performScrollTo().performClick()
        composeRule.onNodeWithTag("corner-x-input").performTextReplacement("5.0")
        composeRule.onNodeWithText("Update corner").performScrollTo().performClick()
        composeRule.onNodeWithText("Apply changes").performScrollTo().performClick()
        Log.i(TAG, "checkpoint applied")
        assertEquals(5.0, store.get("scan")!!.draft.corners[1].x, 0.0)

        composeRule.onNodeWithText("Confirm room").performScrollTo().performClick()
        Log.i(TAG, "checkpoint confirmed")
        assertTrue(store.isConfirmed("scan"))
        assertEquals(5.0, store.get("scan")!!.draft.corners[1].x, 0.0)
    }

    @Test
    fun wall_selection_exposes_only_real_supported_operations() {
        val store = RoomProposalStore()
        store.createFromShell("scan", "space", RoomShellBuildResult.Complete(squareShell()))
        val host = awaitHostActivity()
        composeRule.runOnUiThread { host.setContent { RoomReviewScreen("scan", store) } }

        composeRule.onNodeWithText("Wall 1", substring = true).performScrollTo().performClick()
        composeRule.onNodeWithText("Add opening").assertExists()
        composeRule.onNodeWithText("Split wall").assertExists()
        composeRule.onNodeWithText("Opening profile").assertDoesNotExist()
        composeRule.onNodeWithText("Correct dimension").assertDoesNotExist()
    }

    private fun squareShell(): RoomShell = RoomShell(
        orderedCandidateIds = listOf("bottom", "right", "top", "left"),
        orderedCorners = listOf(
            PointXZ(0.0, 0.0), PointXZ(4.0, 0.0),
            PointXZ(4.0, 3.0), PointXZ(0.0, 3.0),
        ),
        observedCoverageByCandidate = mapOf("bottom" to 1.0, "right" to 1.0, "top" to 1.0, "left" to 1.0),
        confidence = 0.9,
    )

    private fun awaitHostActivity(): RoomReviewTestHostActivity {
        repeat(100) {
            RoomReviewTestHostActivity.current?.let { return it }
            SystemClock.sleep(100)
        }
        error("RoomReviewTestHostActivity was not launched within 10 seconds")
    }

    private companion object {
        const val TAG = "RoomReviewUiTest"
    }
}
