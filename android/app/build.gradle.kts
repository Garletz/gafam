plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.gafam.relay"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.gafam.relay"
        minSdk = 26
        targetSdk = 34
        versionCode = 12
        versionName = "1.3.3-outbox-dedupe"
        ndk {
            abiFilters += listOf("arm64-v8a")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.12.0")
    implementation("androidx.appcompat:appcompat:1.6.1")
    implementation("com.google.android.material:material:1.11.0")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.code.gson:gson:2.10.1")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    // GenAI 0.14 dlopen libonnxruntime.so at runtime (separate AAR on Android)
    implementation("com.microsoft.onnxruntime:onnxruntime-android:1.25.1")
    implementation(files("libs/onnxruntime-genai-android-0.14.0.aar"))
}
