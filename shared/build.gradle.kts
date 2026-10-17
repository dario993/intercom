// Platform-free intercom logic, used by the Android app and the desktop client (composite build).
plugins {
    kotlin("jvm") version "2.2.20"
}

group = "app.intercom"
version = "0.1.0"

kotlin {
    jvmToolchain(17)
}

dependencies {
    api("com.squareup.okhttp3:okhttp:4.12.0")
    compileOnly("org.json:json:20240303") // part of Android; the desktop app adds the real jar

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}
