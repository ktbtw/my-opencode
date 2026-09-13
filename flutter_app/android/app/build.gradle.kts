plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

repositories {
    flatDir {
        dirs("../../../jiguang_sdk/jiguang/libs")
    }
}

android {
    namespace = "com.chatcodex.chat_codex_app"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = "27.0.12077973"

    sourceSets {
        getByName("main") {
            res.srcDirs("src/main/res", "../../../jiguang_sdk/jiguang/src/main/res")
            jniLibs.srcDirs("../../../jiguang_sdk/jiguang/libs")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_11
        targetCompatibility = JavaVersion.VERSION_11
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_11.toString()
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.chatcodex.chat_codex_app"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
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
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

dependencies {
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
