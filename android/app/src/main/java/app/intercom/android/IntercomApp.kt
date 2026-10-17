package app.intercom.android

import android.app.Application

class IntercomApp : Application() {
    val controller by lazy { IntercomController(this) }
}
