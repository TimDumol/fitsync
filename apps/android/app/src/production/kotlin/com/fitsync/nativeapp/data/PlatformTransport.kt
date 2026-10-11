package com.fitsync.nativeapp.data
import android.content.Context
fun createTransport(context: Context, token: suspend (Boolean) -> String): Transport = HttpTransport(token)
