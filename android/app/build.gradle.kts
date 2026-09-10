plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "com.renovex.capture"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.renovex.capture"
        minSdk = 24
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // Task 4: base API URL for the Renovex backend. 10.0.2.2 is the
        // standard Android emulator alias for the host machine's localhost
        // (design spec: Go/Huma remains the authoritative API boundary).
        // A physical device on the same Wi-Fi cannot resolve 10.0.2.2 —
        // use the host machine's LAN IP instead. NOTE: this IP is assigned
        // by DHCP and can change (e.g. after a router/PC reboot) — if the
        // device gets "No route to host", recheck this PC's current IP.
        buildConfigField("String", "API_BASE_URL", "\"http://192.168.1.12:8080\"")
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
        debug {
            // Physical devices cannot reach 10.0.2.2 (emulator-only loopback
            // alias) — a real device on the same Wi-Fi must use the host
            // machine's LAN IP instead. Task 4's stop-condition handoff
            // documents how to override this per your dev machine's actual
            // address when testing on a physical device.
            isDebuggable = true
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.activity:activity-compose:1.9.3")

    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)
    androidTestImplementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    debugImplementation("androidx.compose.ui:ui-tooling")

    implementation("androidx.navigation:navigation-compose:2.8.5")

    // Networking: OkHttp for its persistent-cookie-jar support (needed for
    // the Go backend's httpOnly refresh_token cookie, design spec/Task 1
    // inspection: identity.RegisterHandlers's refresh flow is cookie-only).
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.squareup.retrofit2:retrofit:2.11.0")
    implementation("com.squareup.retrofit2:converter-kotlinx-serialization:2.11.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")

    // Local persistence: DataStore for auth/session and pending-capture
    // state that must survive process death and device restart (design
    // spec §10: offline capture resilience).
    implementation("androidx.datastore:datastore-preferences:1.1.1")

    // Task 5: AR-assisted capture. SceneView wraps ARCore + Filament with a
    // maintained Compose-friendly API (com.google.ar.sceneform is archived/
    // unmaintained) — it is the current standard choice for ARCore-on-
    // Android-Compose per the design spec's "SceneView / Filament" tech
    // stack entry.
    implementation("io.github.sceneview:arsceneview:2.3.0")
    implementation("com.google.ar:core:1.47.0")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
    androidTestImplementation("androidx.compose.ui:ui-test-junit4")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}
