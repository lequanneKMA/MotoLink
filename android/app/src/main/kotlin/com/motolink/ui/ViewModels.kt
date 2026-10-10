package com.motolink.ui

import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import com.motolink.AppContainer
import com.motolink.MotoLinkApp

@Composable
fun container(): AppContainer = (LocalContext.current.applicationContext as MotoLinkApp).container

/** Creates a ViewModel that needs the app container (no DI framework, ADR-018). */
@Composable
inline fun <reified VM : ViewModel> containerViewModel(crossinline create: (AppContainer) -> VM): VM {
    val c = container()
    return viewModel(factory = viewModelFactory { initializer { create(c) } })
}
