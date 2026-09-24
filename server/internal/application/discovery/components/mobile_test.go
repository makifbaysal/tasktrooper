package components

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fileset mirrors application/repository's test `tree` type; a value with a
// trailing "/" key is not representable on an fstest.MapFS (there is no such
// thing as an empty directory), so those fixtures carry a placeholder file
// instead — the mobile detectors only care whether a directory contains
// anything, never whether it is literally empty.
type fileset map[string]string

func treeFrom(t *testing.T, files fileset) *inventory.Tree {
	t.Helper()
	mapFS := fstest.MapFS{}
	for path, content := range files {
		mapFS[path] = &fstest.MapFile{Data: []byte(content)}
	}
	return inventory.FromFS(mapFS)
}

func pbxproj(ids ...string) string {
	body := "// !$*UTF8*$!\n{\n\tobjects = {\n"
	for _, id := range ids {
		body += "\t\tbuildSettings = {\n\t\t\tPRODUCT_BUNDLE_IDENTIFIER = " + id + ";\n\t\t};\n"
	}
	return body + "\t};\n}\n"
}

func plist(bundleID string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>Runner</string>
	<key>CFBundleIdentifier</key>
	<string>` + bundleID + `</string>
</dict>
</plist>
`
}

func TestDetectAppIdentity(t *testing.T) {
	cases := []struct {
		name   string
		layout fileset
		want   domain.AppIdentity
	}{
		{name: "empty directory yields nothing", layout: fileset{}, want: domain.AppIdentity{}},
		{
			name: "flutter app reports both platforms",
			layout: fileset{
				"pubspec.yaml": "name: myapp\nflutter:\n  uses-material-design: true\n",
				"android/app/build.gradle": `android {
    defaultConfig {
        applicationId "com.acme.myapp"
        minSdkVersion 21
    }
}
`,
				"ios/Runner.xcodeproj/project.pbxproj": pbxproj("com.acme.myapp", "com.acme.myapp.RunnerTests"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.myapp", PackageName: "com.acme.myapp"},
		},
		{
			name: "kotlin dsl applicationId with an equals sign",
			layout: fileset{
				"android/app/build.gradle.kts": "android {\n    defaultConfig {\n        applicationId = \"com.acme.kts\"\n    }\n}\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.kts"},
		},
		{
			name: "plain android repo keeps app/build.gradle at the root level",
			layout: fileset{
				"settings.gradle":  "include ':app'\n",
				"app/build.gradle": "android {\n    defaultConfig {\n        applicationId 'com.acme.native'\n    }\n}\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.native"},
		},
		{
			name: "applicationIdSuffix is not an applicationId",
			layout: fileset{
				"app/build.gradle": `android {
    buildTypes {
        debug {
            applicationIdSuffix ".debug"
        }
    }
}
`,
			},
			want: domain.AppIdentity{},
		},
		{
			name: "a commented-out applicationId is ignored",
			layout: fileset{
				"app/build.gradle": "android {\n    // applicationId \"com.acme.old\"\n    defaultConfig {\n        applicationId \"com.acme.current\"\n    }\n}\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.current"},
		},
		{
			name: "an interpolated applicationId is refused rather than guessed",
			layout: fileset{
				"app/build.gradle": "android {\n    defaultConfig {\n        applicationId \"com.acme.${flavor}\"\n    }\n}\n",
			},
			want: domain.AppIdentity{},
		},
		{
			name: "manifest package is the fallback when gradle names no applicationId",
			layout: fileset{
				"app/build.gradle": "android {\n    namespace 'com.acme.lib'\n}\n",
				"app/src/main/AndroidManifest.xml": `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="com.acme.frommanifest">
    <application android:label="app" />
</manifest>
`,
			},
			want: domain.AppIdentity{PackageName: "com.acme.frommanifest"},
		},
		{
			name: "a queries <package> child is not the manifest's own package",
			layout: fileset{
				"app/src/main/AndroidManifest.xml": `<manifest xmlns:android="http://schemas.android.com/apk/res/android">
    <queries>
        <package android:name="com.other.app" />
    </queries>
</manifest>
`,
			},
			want: domain.AppIdentity{},
		},
		{
			name: "gradle applicationId wins over the manifest package",
			layout: fileset{
				"android/app/build.gradle":                      "android {\n    defaultConfig {\n        applicationId \"com.acme.shipped\"\n    }\n}\n",
				"android/app/src/main/AndroidManifest.xml":      `<manifest package="com.acme.namespace" />`,
				"android/app/src/debug/AndroidManifest.xml":     `<manifest package="com.acme.debug" />`,
				"android/app/src/profile/AndroidManifest.xml":   `<manifest package="com.acme.profile" />`,
				"android/gradle/wrapper/gradle-wrapper.propert": "distributionUrl=x\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.shipped"},
		},
		{
			name: "native ios project at the root, quoted bundle id",
			layout: fileset{
				"MyApp.xcodeproj/project.pbxproj": pbxproj(`"com.acme.ios"`),
			},
			want: domain.AppIdentity{BundleID: "com.acme.ios"},
		},
		{
			name: "test and extension targets never win over the app",
			layout: fileset{
				"ios/Runner.xcodeproj/project.pbxproj": pbxproj(
					"com.acme.app.RunnerTests",
					"com.acme.app.RunnerUITests",
					"com.acme.app.NotificationService",
					"com.acme.app",
					"com.acme.app.watchkitapp",
				),
			},
			want: domain.AppIdentity{BundleID: "com.acme.app"},
		},
		{
			name: "an independently named widget target does not win either",
			layout: fileset{
				"MyApp.xcodeproj/project.pbxproj": pbxproj("com.acme.MyAppWidgetExtension", "com.acme.myapp"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.myapp"},
		},
		{
			name: "a pbxproj that only names variables falls through to Info.plist",
			layout: fileset{
				"ios/Runner.xcodeproj/project.pbxproj": pbxproj("$(PRODUCT_BUNDLE_IDENTIFIER)", "$(PRODUCT_BUNDLE_IDENTIFIER).RunnerTests"),
				"ios/Runner/Info.plist":                plist("com.acme.fromplist"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.fromplist"},
		},
		{
			name: "a placeholder Info.plist yields nothing rather than the variable",
			layout: fileset{
				"ios/Runner/Info.plist": plist("$(PRODUCT_BUNDLE_IDENTIFIER)"),
			},
			want: domain.AppIdentity{},
		},
		{
			name: "an Info.plist in a native app's target folder is found",
			layout: fileset{
				"MyApp/Info.plist": plist("com.acme.nativeplist"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.nativeplist"},
		},
		{
			name: "an unparseable pbxproj is not an error, just no answer",
			layout: fileset{
				"ios/Runner.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{ truncated",
			},
			want: domain.AppIdentity{},
		},
		{
			name: "a bare word is not an identifier",
			layout: fileset{
				"app/build.gradle":              "android {\n    defaultConfig {\n        applicationId \"myapp\"\n    }\n}\n",
				"App.xcodeproj/project.pbxproj": pbxproj("Runner"),
			},
			want: domain.AppIdentity{},
		},
		{
			name: "a backend gradle project claims no package name",
			layout: fileset{
				"build.gradle": "plugins { id 'java' }\ndependencies { implementation 'org.springframework:spring-core' }\n",
			},
			want: domain.AppIdentity{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, detectAppIdentity(treeFrom(t, tc.layout), "."))
		})
	}
}

const xcscheme = `<?xml version="1.0" encoding="UTF-8"?><Scheme LastUpgradeVersion="1500" version="1.7"></Scheme>`

const (
	appGradle = "plugins {\n    id 'com.android.application'\n}\n\nandroid {\n    namespace 'com.acme.app'\n}\n"
	libGradle = "plugins {\n    id 'com.android.library'\n}\n"
)

func TestDetectBuildTargets(t *testing.T) {
	cases := []struct {
		name   string
		layout fileset
		want   domain.BuildTargets
	}{
		{name: "empty directory states nothing", layout: fileset{}, want: domain.BuildTargets{}},
		{
			name:   "the one shared scheme is the answer",
			layout: fileset{"MyApp.xcodeproj/xcshareddata/xcschemes/MyApp.xcscheme": xcscheme},
			want:   domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name:   "a shared scheme unrelated to the project name still wins",
			layout: fileset{"MyApp.xcodeproj/xcshareddata/xcschemes/Production.xcscheme": xcscheme},
			want:   domain.BuildTargets{XcodeScheme: "Production"},
		},
		{
			name: "test and UI-test schemes are not the app",
			layout: fileset{
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyApp.xcscheme":        xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyAppTests.xcscheme":   xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyAppUITests.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "an app extension scheme is not the app either",
			layout: fileset{
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyApp.xcscheme":                xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyAppWidgetExtension.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "two unrelated shared schemes leave it unanswered",
			layout: fileset{
				"MyApp.xcodeproj/xcshareddata/xcschemes/Staging.xcscheme":    xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/Production.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{},
		},
		{
			name:   "the project name answers only when nothing is shared",
			layout: fileset{"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{}\n"},
			want:   domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "a workspace wrapping its project is still one name",
			layout: fileset{
				"MyApp.xcodeproj/project.pbxproj":            "// !$*UTF8*$!\n{}\n",
				"MyApp.xcworkspace/contents.xcworkspacedata": "<Workspace></Workspace>",
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "two unrelated projects and no shared scheme leave it unanswered",
			layout: fileset{
				"MyApp.xcodeproj/project.pbxproj":    "// !$*UTF8*$!\n{}\n",
				"OtherApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{}\n",
			},
			want: domain.BuildTargets{},
		},
		{
			name: "the single included Android module is the answer",
			layout: fileset{
				"settings.gradle":  "rootProject.name = 'acme'\ninclude ':app'\n",
				"app/build.gradle": appGradle,
				"gradlew":          "#!/bin/sh\n",
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "library modules are not the thing that bundles",
			layout: fileset{
				"settings.gradle":       "include ':app', ':core', ':data'\n",
				"app/build.gradle":      appGradle,
				"core/build.gradle":     libGradle,
				"data/build.gradle.kts": "plugins {\n    id(\"com.android.library\")\n}\n",
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "two application modules leave it unanswered",
			layout: fileset{
				"settings.gradle":   "include ':app'\ninclude ':wear'\n",
				"app/build.gradle":  appGradle,
				"wear/build.gradle": appGradle,
			},
			want: domain.BuildTargets{},
		},
		{
			name: "a module the settings file never includes is not in the build",
			layout: fileset{
				"settings.gradle":     "include ':app'\n",
				"app/build.gradle":    appGradle,
				"sample/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "a commented-out include is not an include",
			layout: fileset{
				"settings.gradle":   "include ':app'\n// include ':wear'\n/* include ':tv' */\n",
				"app/build.gradle":  appGradle,
				"wear/build.gradle": appGradle,
				"tv/build.gradle":   appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "the Kotlin DSL, a multi-line include and a catalog alias",
			layout: fileset{
				"settings.gradle.kts":         "rootProject.name = \"acme\"\ninclude(\n    \":androidApp\",\n    \":shared\",\n)\n",
				"androidApp/build.gradle.kts": "plugins {\n    alias(libs.plugins.android.application)\n}\n",
				"shared/build.gradle.kts":     "plugins {\n    alias(libs.plugins.android.library)\n}\n",
			},
			want: domain.BuildTargets{GradleModule: "androidApp"},
		},
		{
			name: "a nested module keeps its colons",
			layout: fileset{
				"settings.gradle":           "include ':apps:android'\ninclude ':libs:core'\n",
				"apps/android/build.gradle": appGradle,
				"libs/core/build.gradle":    libGradle,
			},
			want: domain.BuildTargets{GradleModule: "apps:android"},
		},
		{
			name: "includeBuild is not include",
			layout: fileset{
				"settings.gradle":          "includeBuild('build-logic')\ninclude ':app'\n",
				"app/build.gradle":         appGradle,
				"build-logic/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "the Flutter layout answers both halves",
			layout: fileset{
				"pubspec.yaml": "name: acme\nflutter:\n  sdk: flutter\n",
				"ios/Runner.xcodeproj/xcshareddata/xcschemes/Runner.xcscheme": xcscheme,
				"ios/Runner.xcworkspace/contents.xcworkspacedata":             "<Workspace></Workspace>",
				"android/settings.gradle":                                     "include ':app'\n",
				"android/app/build.gradle":                                    appGradle,
			},
			want: domain.BuildTargets{XcodeScheme: "Runner", GradleModule: "app"},
		},
		{
			name: "one half can answer while the other stays empty",
			layout: fileset{
				"android/settings.gradle":  "include ':app'\n",
				"android/app/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name:   "the conventional app module answers without a settings file",
			layout: fileset{"android/app/build.gradle": appGradle},
			want:   domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "a Gradle JVM backend is not an Android app",
			layout: fileset{
				"settings.gradle":      "include ':service'\n",
				"service/build.gradle": "plugins {\n    id 'java'\n}\n",
			},
			want: domain.BuildTargets{},
		},
		{
			name:   "a scheme name that would need shell escaping is not answered",
			layout: fileset{"MyApp.xcodeproj/xcshareddata/xcschemes/My$App.xcscheme": xcscheme},
			want:   domain.BuildTargets{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, detectBuildTargets(treeFrom(t, tc.layout), "."))
		})
	}
}

func TestDetectMobilePlatform(t *testing.T) {
	cases := []struct {
		name   string
		layout fileset
		want   string
	}{
		{name: "nothing recognisable", layout: fileset{"README.md": "hi\n"}, want: ""},
		{
			name:   "flutter app is cross platform",
			layout: fileset{"pubspec.yaml": "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n"},
			want:   domain.MobilePlatformCross,
		},
		{
			name: "react native layout is cross platform",
			layout: fileset{
				"android/.gitkeep": "",
				"ios/.gitkeep":     "",
				"package.json":     `{"dependencies":{"react-native":"0.73.0"}}`,
			},
			want: domain.MobilePlatformCross,
		},
		{
			name:   "xcode project alone is ios",
			layout: fileset{"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "xcode workspace alone is ios",
			layout: fileset{"MyApp.xcworkspace/contents.xcworkspacedata": "<Workspace/>"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "xcodegen spec alone is ios",
			layout: fileset{"project.yml": "name: MyApp\n"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "swift package manifest alone is ios",
			layout: fileset{"Package.swift": "// swift-tools-version:5.9"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "gradle wrapper and manifest alone is android",
			layout: fileset{"gradlew": "#!/bin/sh\n", "app/src/main/AndroidManifest.xml": "<manifest/>"},
			want:   domain.MobilePlatformAndroid,
		},
		{
			name:   "settings.gradle.kts alone is android",
			layout: fileset{"settings.gradle.kts": `rootProject.name = "app"`},
			want:   domain.MobilePlatformAndroid,
		},
		{
			name:   "an ios folder without an android one is ios",
			layout: fileset{"ios/.gitkeep": "", "package.json": `{"name":"app"}`},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "both native toolchains in one tree is cross platform",
			layout: fileset{"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!", "build.gradle": "// android\n"},
			want:   domain.MobilePlatformCross,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectMobilePlatform(treeFrom(t, tc.layout), ".", nil)
			require.Equal(t, tc.want, got)
			if got != "" {
				require.True(t, domain.ValidMobilePlatform(got))
			}
		})
	}
}
