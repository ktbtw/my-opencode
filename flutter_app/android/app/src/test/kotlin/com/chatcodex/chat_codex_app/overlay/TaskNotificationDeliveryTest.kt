package com.chatcodex.chat_codex_app.overlay

import android.app.Application
import android.app.AppComponentFactory
import android.content.BroadcastReceiver
import android.content.Intent
import android.app.NotificationManager
import android.content.Context
import android.os.Looper
import com.chatcodex.chat_codex_app.MainActivity
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.annotation.Implements
import org.robolectric.annotation.Implementation
import java.net.HttpURLConnection
import java.net.ServerSocket
import java.net.InetAddress
import java.net.URL
import java.time.Duration
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class, shadows = [FixtureComponentFactory::class])
@GraphicsMode(GraphicsMode.Mode.LEGACY)
class TaskNotificationDeliveryTest {
    private lateinit var service: GlobalOverlayService
    private lateinit var manager: NotificationManager

    @Before
    fun setUp() {
        service = Robolectric.buildService(GlobalOverlayService::class.java).get()
        manager = service.getSystemService(NotificationManager::class.java)
        service.createNotificationChannel()
        val prefs = service.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        prefs.edit().putString("flutter.operator_key", "fixture-account").commit()
        service.loadNotificationCursorScope(prefs, "https://fixture.test")
        foreground(false)
    }

    @After
    fun tearDown() {
        foreground(false)
        service.destroyed = true
        service.handler.removeCallbacksAndMessages(null)
        service.executor.shutdownNow()
    }

    private fun foreground(value: Boolean) {
        MainActivity::class.java.getDeclaredField("appForeground").apply {
            isAccessible = true
            setBoolean(null, value)
        }
    }

    private fun completion(version: Long, task: String = "fixture-task", unread: Boolean = true): JSONObject =
        JSONObject().put("version", version).put("operation_id", "chat-task:$task")
            .put("record", JSONObject().put("status", "succeeded").put("unread", unread)
                .put("title", "完成").put("message", "任务结果")
                .put("metadata", JSONObject().put("task_id", task)))

    @Test
    fun backgroundCompletionPostsExactlyOnce() {
        val change = completion(1)
        service.applyNotificationUpdate(change)
        assertNotNull(shadowOf(manager).getNotification("fixture-task".hashCode()))
        assertEquals(1L, service.notificationCursor)
        service.applyNotificationUpdate(change)
        assertEquals(1, shadowOf(manager).allNotifications.size)
    }

    @Test
    fun homeTransitionRechecksBeforeConsumingForegroundCompletion() {
        foreground(true)
        service.applyNotificationUpdate(completion(1))
        assertEquals(0L, service.notificationCursor)
        assertEquals(1, service.pendingNotificationChanges.size)
        assertNull(shadowOf(manager).getNotification("fixture-task".hashCode()))
        foreground(false)
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(800))
        assertNotNull(shadowOf(manager).getNotification("fixture-task".hashCode()))
        assertEquals(1L, service.notificationCursor)
        assertTrue(service.pendingNotificationChanges.isEmpty())
    }

    @Test
    fun confirmedForegroundDoesNotPostBackgroundAlert() {
        foreground(true)
        service.applyNotificationUpdate(completion(1))
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(800))
        assertNull(shadowOf(manager).getNotification("fixture-task".hashCode()))
        assertEquals(1L, service.notificationCursor)
        assertFalse(service.notificationDeliveryBlocked)
    }

    @Test
    fun laterChangesDoNotPostponeTheTransitionRecheck() {
        foreground(true)
        service.applyNotificationUpdate(completion(1, "first"))
        val firstRetryAt = service.notificationRetryScheduledAt
        service.applyNotificationUpdate(completion(2, "second"))
        assertEquals(firstRetryAt, service.notificationRetryScheduledAt)
        foreground(false)
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(800))
        assertEquals(2, shadowOf(manager).allNotifications.size)
        assertEquals(2L, service.notificationCursor)
    }

    @Test
    fun blockedPermissionRetainsCursorAndRetriesAfterGrant() {
        shadowOf(manager).setNotificationsEnabled(false)
        service.applyNotificationUpdate(completion(1))
        service.applyNotificationUpdate(completion(2, "another"))
        assertEquals(0L, service.notificationCursor)
        assertEquals(2, service.pendingNotificationChanges.size)
        shadowOf(manager).setNotificationsEnabled(true)
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(16))
        assertEquals(2, shadowOf(manager).allNotifications.size)
        assertEquals(2L, service.notificationCursor)
        assertTrue(service.pendingNotificationChanges.isEmpty())
    }

    @Test
    fun readReceiptRemovesDeferredCompletionWithoutAlert() {
        foreground(true)
        service.applyNotificationUpdate(completion(1))
        service.applyNotificationUpdate(completion(2, unread = false))
        foreground(false)
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(16))
        assertTrue(shadowOf(manager).allNotifications.isEmpty())
        assertEquals(2L, service.notificationCursor)
        assertTrue(service.pendingNotificationChanges.isEmpty())
    }

    @Test
    fun blockedNotificationsSurviveServiceRecreation() {
        shadowOf(manager).setNotificationsEnabled(false)
        service.applyNotificationUpdate(completion(1))
        service.handler.removeCallbacksAndMessages(null)
        service.executor.shutdownNow()
        service = Robolectric.buildService(GlobalOverlayService::class.java).get()
        val prefs = service.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        service.loadNotificationCursorScope(prefs, "https://fixture.test")
        service.loadPendingNotificationChanges(prefs)
        assertEquals(1, service.pendingNotificationChanges.size)
        assertEquals(0L, service.notificationCursor)
        shadowOf(manager).setNotificationsEnabled(true)
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(16))
        assertNotNull(shadowOf(manager).getNotification("fixture-task".hashCode()))
        assertEquals(1L, service.notificationCursor)
    }

    @Test
    fun networkChangeDisconnectsAnAlreadyRunningHalfOpenStream() {
        val disconnected = CountDownLatch(1)
        service.streamLoopRunning = true
        service.streamConnection = object : HttpURLConnection(URL("https://fixture.test/events")) {
            override fun disconnect() { disconnected.countDown() }
            override fun usingProxy() = false
            override fun connect() {}
        }
        service.ensureHealthyEventStream("test-network-change", networkChanged = true)
        assertTrue(disconnected.await(3, TimeUnit.SECONDS))
    }

    @Test
    fun durableFeedBackfillsWhileLiveStreamIsUnavailable() {
        val server = HttpFixture {
            JSONObject().put("items", JSONArray().put(completion(2))).put("cursor", 2)
        }
        try {
            val prefs = service.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
            prefs.edit().putString("flutter.base_url", "http://127.0.0.1:${server.port}")
                .putString("flutter.access_token", "fixture-token").commit()
            service.recordNotificationCursor(1)
            service.reconcileNotificationFeed("test-offline-sse")
            awaitReconciliation()
            assertNotNull(shadowOf(manager).getNotification("fixture-task".hashCode()))
            assertEquals(2L, service.notificationCursor)
        } finally { server.close() }
    }

    @Test
    fun paginatedRecoveryCoalescesReadReceiptBeforePosting() {
        val requests = AtomicInteger()
        val server = HttpFixture {
            if (requests.get() == 0) assertTrue(it.contains("after=1"))
            else assertTrue(it.contains("after=201"))
            val page = if (requests.getAndIncrement() == 0) {
                val items = JSONArray().put(completion(2))
                for (version in 3L..201L) items.put(JSONObject().put("version", version)
                    .put("operation_id", "other:$version").put("record", JSONObject().put("unread", true).put("status", "running")))
                JSONObject().put("items", items).put("cursor", 201)
            } else {
                JSONObject().put("items", JSONArray().put(completion(202, unread = false)))
                    .put("cursor", 202)
            }
            page
        }
        try {
            service.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE).edit()
                .putString("flutter.base_url", "http://127.0.0.1:${server.port}")
                .putString("flutter.access_token", "fixture-token").commit()
            service.recordNotificationCursor(1)
            service.reconcileNotificationFeed("test-pagination")
            awaitReconciliation()
            assertEquals(2, requests.get())
            assertTrue(shadowOf(manager).allNotifications.isEmpty())
            assertEquals(202L, service.notificationCursor)
        } finally { server.close() }
    }

    @Test
    fun pendingQueueIsIsolatedAcrossAccounts() {
        shadowOf(manager).setNotificationsEnabled(false)
        service.applyNotificationUpdate(completion(1))
        val prefs = service.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        prefs.edit().putString("flutter.operator_key", "second-account").commit()
        service.loadNotificationCursorScope(prefs, "https://fixture.test")
        assertTrue(service.pendingNotificationChanges.isEmpty())
        assertEquals(0L, service.notificationCursor)
        prefs.edit().putString("flutter.operator_key", "fixture-account").commit()
        service.loadNotificationCursorScope(prefs, "https://fixture.test")
        assertEquals(1, service.pendingNotificationChanges.size)
        assertTrue(service.notificationDeliveryBlocked)
    }

    @Test
    fun staleStreamEventsFromPreviousAccountAreIgnored() {
        val oldScope = service.notificationCursorScope
        val prefs = service.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        prefs.edit().putString("flutter.operator_key", "second-account").commit()
        service.loadNotificationCursorScope(prefs, "https://fixture.test")
        service.dispatchStreamEvent("notification.updated", completion(1).toString(), oldScope)
        shadowOf(Looper.getMainLooper()).idle()
        assertTrue(shadowOf(manager).allNotifications.isEmpty())
        assertEquals(0L, service.notificationCursor)
    }

    /** Small real HTTP fixture, including real socket reads (no mocked API layer). */
    private class HttpFixture(val respond: (String) -> JSONObject) : AutoCloseable {
        private val socket = ServerSocket(0, 50, InetAddress.getByName("127.0.0.1"))
        val port: Int get() = socket.localPort
        @Volatile var failure: Throwable? = null
        private val worker = kotlin.concurrent.thread(isDaemon = true, name = "notification-http-fixture") {
            try {
                while (!socket.isClosed) {
                    socket.accept().use { client ->
                        client.soTimeout = 3000
                        val reader = client.getInputStream().bufferedReader()
                        val request = reader.readLine().orEmpty()
                        while (!reader.readLine().isNullOrEmpty()) { /* consume headers */ }
                        val body = respond(request).toString().toByteArray(Charsets.UTF_8)
                        val headers = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\nConnection: close\r\n\r\n"
                        client.getOutputStream().write(headers.toByteArray(Charsets.US_ASCII))
                        client.getOutputStream().write(body)
                        client.getOutputStream().flush()
                    }
                }
            } catch (error: Throwable) { if (!socket.isClosed) failure = error }
        }
        override fun close() {
            socket.close()
            worker.join(3000)
            failure?.let { throw AssertionError("HTTP fixture failed", it) }
        }
    }

    private fun awaitReconciliation() {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5)
        while (service.notificationReconcileRunning && System.nanoTime() < deadline) Thread.sleep(10)
        assertFalse("Reconciliation timed out", service.notificationReconcileRunning)
        shadowOf(Looper.getMainLooper()).idle()
    }
}

/** Vendor push receivers are unrelated to the native completion delivery fixture. */
@Implements(AppComponentFactory::class)
class FixtureComponentFactory {
    @Implementation
    fun instantiateReceiver(classLoader: ClassLoader, className: String, intent: Intent?): BroadcastReceiver =
        object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {}
        }
}
