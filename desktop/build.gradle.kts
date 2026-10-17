plugins {
    kotlin("jvm") version "2.2.20"
    id("org.jetbrains.kotlin.plugin.compose") version "2.2.20"
    id("org.jetbrains.compose") version "1.9.3"
}

group = "app.intercom"
version = "0.1.0"

kotlin {
    jvmToolchain(17)
}

dependencies {
    implementation("app.intercom:shared")
    // Windows x64 natives even when built elsewhere: the control-room PC is x64.
    implementation(compose.desktop.windows_x64)
    implementation(compose.material3)
    implementation(compose.materialIconsExtended)
    implementation("org.json:json:20240303")
    implementation("io.github.jaredmdobson:concentus:1.0.2")
    implementation("org.jmdns:jmdns:3.6.3")
    implementation("net.java.dev.jna:jna-platform:5.19.1")

    testImplementation("junit:junit:4.13.2")
}

// `./gradlew test` also runs the shared module's tests.
tasks.test {
    dependsOn(gradle.includedBuild("shared").task(":test"))
}

compose.desktop {
    application {
        mainClass = "app.intercom.desktop.MainKt"
    }
}

/** Audio latency and Opus interop probe (Task 2): `./gradlew runProbe --args="--list"`. */
tasks.register<JavaExec>("runProbe") {
    group = "application"
    mainClass = "app.intercom.desktop.probe.ProbeKt"
    classpath = sourceSets.main.get().runtimeClasspath
    standardInput = System.`in`
}
