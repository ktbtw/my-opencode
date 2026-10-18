import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    id("org.jetbrains.kotlin.plugin.compose")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// release 签名材料随私有仓库提交（android/key.properties + android/keystore/），
// 任何构建机 clone 后产出的 APK 签名都一致；缺少文件时直接失败，不再静默退回 debug 签名。
val keystorePropertiesFile = rootProject.file("key.properties")
if (!keystorePropertiesFile.exists()) {
    throw GradleException(
        "缺少 ${keystorePropertiesFile.path}：release 签名依赖它（见 更新发布指南.md「Android 签名」）"
    )
}
val keystoreProperties = Properties().apply {
    keystorePropertiesFile.inputStream().use { load(it) }
}

repositories {
    flatDir {
        dirs("../../../jiguang_sdk/jiguang/libs")
    }
}

android {
    namespace = "com.chatcodex.chat_codex_app"
    compileSdk = 36
    ndkVersion = "27.0.12077973"

    signingConfigs {
        create("release") {
            storeFile = rootProject.file(keystoreProperties.getProperty("storeFile"))
            storePassword = keystoreProperties.getProperty("storePassword")
            keyAlias = keystoreProperties.getProperty("keyAlias")
            keyPassword = keystoreProperties.getProperty("keyPassword")
        }
    }

    sourceSets {
        getByName("main") {
            res.srcDirs("src/main/res", "../../../jiguang_sdk/jiguang/src/main/res")
            jniLibs.srcDirs("../../../jiguang_sdk/jiguang/libs")
        }
    }

    lint {
        // AGP 8.7 的 lint 与 Kotlin 2.3 的分析 API 不兼容，该检查会直接崩溃
        // （NonNullableMutableLiveDataDetector / KaCallableMemberCall），先禁用。
        disable += "NullSafeMutableLiveData"
    }

    buildFeatures {
        compose = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_11
        targetCompatibility = JavaVersion.VERSION_11
    }

    testOptions {
        unitTests.isIncludeAndroidResources = true
        unitTests.all {
            it.systemProperty("robolectric.dependency.repo.url", "https://repo.maven.apache.org/maven2")
        }
    }

    // Kotlin 2.3 起 kotlinOptions 已移除，改用 compilerOptions DSL。
    kotlin {
        compilerOptions {
            jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_11)
        }
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.chatcodex.chat_codex_app"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        // Jetpack Compose（androidx.compose.animation-core）要求 minSdk >= 23，
        // 悬浮助手 UI 依赖 Compose，因此这里显式抬到 24。
        minSdk = 24
        ndk {
            // 只保留真机 ABI，去掉模拟器用的 x86_64，减小包体
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        manifestPlaceholders["JPUSH_PKGNAME"] = applicationId!!
        manifestPlaceholders["JPUSH_APPKEY"] = "ab81912767ea58edcfc9ff89"
        manifestPlaceholders["JPUSH_CHANNEL"] = "developer-default"
        manifestPlaceholders["XIAOMI_APPKEY"] = ""
        manifestPlaceholders["XIAOMI_APPID"] = ""
        manifestPlaceholders["NIO_APPID"] = ""
        manifestPlaceholders["MEIZU_APPKEY"] = ""
        manifestPlaceholders["MEIZU_APPID"] = ""
        manifestPlaceholders["OPPO_APPKEY"] = ""
        manifestPlaceholders["OPPO_APPID"] = ""
        manifestPlaceholders["OPPO_APPSECRET"] = ""
        manifestPlaceholders["VIVO_APPKEY"] = ""
        manifestPlaceholders["VIVO_APPID"] = ""
        manifestPlaceholders["HONOR_APPID"] = ""
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName("release")
        }
    }
}

dependencies {
    // 悬浮助手 UI 用 Jetpack Compose 重写（对齐 Operit 的交互与视觉）。
    implementation(platform("androidx.compose:compose-bom:2024.12.01"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.animation:animation")
    implementation("androidx.activity:activity-compose:1.8.2")
    // 屏幕 OCR：内置中文识别模型，离线可用（国内设备拉不到 Play 服务的按需模块）
    implementation("com.google.mlkit:text-recognition-chinese:16.0.0")
    // 液态玻璃（Android 13+ 真模糊，低版本自动降级）
    implementation("io.github.kyant0:backdrop:1.0.6")

    testImplementation("junit:junit:4.13.2")
    // Notification delivery uses legacy Android shadows, not native graphics.
    testImplementation("org.robolectric:robolectric:4.12.2") {
        exclude(group = "org.robolectric", module = "nativeruntime-dist-compat")
    }
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jcore-android-5.3.1.aar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/push-internal-5.0.5.aar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/HiPushSDK-8.0.12.307.aar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/com.heytap.msp_V3.7.1.aar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/niopush-sdk-v1.0.aar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-huawei-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-xiaomi-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-oppo-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-vivo-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-honor-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-meizu-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/jpush-android-plugin-nio-v6.0.1.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/MiPush_SDK_Client_6_0_1-C.jar"))
    implementation(files("../../../jiguang_sdk/jiguang/libs/push_sdk_v4.1.0.0_510.jar"))
}

flutter {
    source = "../.."
}
