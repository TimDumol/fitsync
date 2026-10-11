package com.fitsync.nativeapp

import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.lifecycle.ViewModelProvider
import android.os.Looper
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.Shadows.shadowOf
import java.time.Duration

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = FitSyncApplication::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ScreenTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()
    private fun ready() {
        assumeTrue(BuildConfig.FIXTURES)
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("today-screen").fetchSemanticsNodes().isNotEmpty() }
    }
    private fun click(tag: String) {
        try { compose.waitUntil(10_000) {
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
            compose.onAllNodes(hasTestTag(tag) and isEnabled()).fetchSemanticsNodes().isNotEmpty()
        } } catch (error: Throwable) {
            println("Waiting to click $tag: " + ViewModelProvider(compose.activity)[AppModel::class.java].state.value)
            println(LogBook.entries.value.joinToString("\n")); throw error
        }
        compose.onNodeWithTag(tag).performScrollTo().performClick()
    }
    private fun await(tag: String) {
        try { compose.waitUntil(10_000) {
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(100))
            compose.onAllNodesWithTag(tag).fetchSemanticsNodes().isNotEmpty()
        } }
        catch (error: Throwable) {
            println(ViewModelProvider(compose.activity)[AppModel::class.java].state.value)
            println(LogBook.entries.value.joinToString("\n"))
            compose.onRoot().printToLog("native-test")
            throw error
        }
    }
    @Test fun nativeCoreWorkflow() {
        ready()
        workout()
        garmin()
        planAndLogout()
    }
    private fun workout() {
        click("start-workout-button")
        await("planned-session-screen")
        compose.onNodeWithTag("session-start-workout-button").performScrollTo()
        click("session-start-workout-button")
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("workout-entry-1-reps").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("workout-entry-1-reps").performTextReplacement("10")
        compose.onNodeWithTag("workout-entry-1-load").performTextReplacement("18")
        compose.onNodeWithTag("workout-entry-1-rest-button").performScrollTo().performClick()
        compose.onNodeWithTag("rest-timer").assertExists()
        compose.onNodeWithTag("stop-rest-button").performClick()
        compose.onNodeWithTag("undo-rest-button").assertExists().performClick()
        compose.onNodeWithTag("rest-timer").assertExists()
        compose.onNodeWithTag("stop-rest-button").performClick()
        compose.onNodeWithTag("add-set-button").performClick()
        compose.onNodeWithTag("add-unplanned-button").performClick()
        compose.onNodeWithTag("add-exercise-confirm").assertIsNotEnabled()
        compose.onNodeWithTag("add-exercise-name").performTextInput("Cable curl")
        compose.onNodeWithTag("add-exercise-confirm").performClick()
        compose.onNodeWithText("Cable curl · Set 1").assertExists()
        compose.onNodeWithTag("workout-notes-input").performScrollTo().performTextInput("Native offline workout")
        compose.onNodeWithTag("finish-workout-button").performScrollTo().performClick()
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("workout-summary-screen").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("Workout is pending sync.").assertExists()
        compose.onNodeWithText("Native offline workout").assertExists()
        compose.onNodeWithTag("delete-workout-button").performScrollTo()
        click("delete-workout-button")
        compose.onNodeWithText("Delete", useUnmergedTree = true).performClick()
        await("history-screen")
    }
    private fun planAndLogout() {
        compose.onNodeWithText("Plan", useUnmergedTree = true).performClick()
        compose.onNodeWithTag("planned-session-row").performScrollTo().performClick()
        compose.onNodeWithTag("session-skip-button").performScrollTo().performClick()
        compose.waitUntil(20_000) { compose.onAllNodes(hasText("skipped", substring = true)).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("session-skip-button").assertDoesNotExist()
        compose.onNodeWithText("Back").performClick()
        compose.onNodeWithText("Settings", useUnmergedTree = true).performClick()
        compose.onNodeWithTag("logout-button").performScrollTo()
        click("logout-button")
        await("sign-in-screen")
    }
    private fun garmin() {
        compose.onNodeWithText("Settings", useUnmergedTree = true).performClick()
        compose.onNodeWithTag("garmin-email").performScrollTo().performTextInput("mfa")
        compose.onNodeWithTag("garmin-password").performScrollTo().performTextInput("fixturepassword")
        compose.onNodeWithTag("connect-garmin-button").performScrollTo()
        click("connect-garmin-button")
        await("garmin-mfa-code")
        compose.onNodeWithTag("garmin-mfa-code").performScrollTo().performTextInput("123456")
        compose.onNodeWithTag("verify-garmin-button").performScrollTo()
        click("verify-garmin-button")
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("garmin-connected").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("sync-garmin-button").performScrollTo()
        click("sync-garmin-button")
        await("garmin-result")
        compose.onNodeWithTag("disconnect-garmin-button").performScrollTo().performClick()
        compose.onNodeWithText("Disconnect", useUnmergedTree = true).performClick()
        compose.waitUntil(20_000) { compose.onAllNodesWithTag("garmin-connected").fetchSemanticsNodes().isEmpty() }
    }
}
