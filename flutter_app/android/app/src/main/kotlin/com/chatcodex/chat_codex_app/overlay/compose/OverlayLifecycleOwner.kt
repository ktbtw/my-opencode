package com.chatcodex.chat_codex_app.overlay.compose

import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.LifecycleRegistry
import androidx.savedstate.SavedStateRegistry
import androidx.savedstate.SavedStateRegistryController
import androidx.savedstate.SavedStateRegistryOwner

/**
 * WindowManager 里的 ComposeView 需要一个 LifecycleOwner 与 SavedStateRegistryOwner，
 * 否则挂载时会抛 ViewTreeLifecycleOwner not found。
 */
class OverlayLifecycleOwner : LifecycleOwner, SavedStateRegistryOwner {
    private val registry = LifecycleRegistry(this)
    private val savedStateController = SavedStateRegistryController.create(this)
    private var attached = false

    override val lifecycle: Lifecycle get() = registry

    override val savedStateRegistry: SavedStateRegistry
        get() = savedStateController.savedStateRegistry

    fun attach() {
        if (attached) return
        attached = true
        savedStateController.performRestore(null)
        registry.currentState = Lifecycle.State.RESUMED
    }

    fun detach() {
        if (!attached) return
        attached = false
        registry.currentState = Lifecycle.State.DESTROYED
    }
}
