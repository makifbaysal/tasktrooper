package prompt

var mobileNotConfiguredKey = Define[struct{}]("guard.mobile_not_configured", struct{}{})
var mobileDeviceBusyKey = Define[struct{}]("guard.mobile_device_busy", struct{}{})
var mobileSelectorMissingKey = Define[struct{}]("guard.mobile_selector_missing", struct{}{})
var mobileTapSelectorMissingKey = Define[struct{}]("guard.mobile_tap_selector_missing", struct{}{})

type mobileLaunchNoPackageInput struct{ Env string }

var mobileLaunchNoPackageKey = Define("guard.mobile_launch_no_package", mobileLaunchNoPackageInput{Env: "stage"})

type mobileWaitForTimeoutInput struct {
	Describe       string
	TimeoutSeconds int
}

var mobileWaitForTimeoutKey = Define("tool_results.mobile_wait_for_timeout", mobileWaitForTimeoutInput{Describe: "text \"Save\"", TimeoutSeconds: 10})

type mobileScreenshotTooLargeInput struct{ Bytes int }

var mobileScreenshotTooLargeKey = Define("tool_results.mobile_screenshot_too_large", mobileScreenshotTooLargeInput{Bytes: 2000000})

var mobileLaunchHoldDeviceKey = Define[struct{}]("tool_results.mobile_launch_hold_device", struct{}{})

// MobileNotConfiguredText is what every mobile_* tool says when no operator
// has attached a device to this build.
func MobileNotConfiguredText() string { return Text(mobileNotConfiguredKey) }

// MobileDeviceBusyContent is the ResourceBlock content handed back instead of
// a tool error when the shared test device is already leased to another run.
func MobileDeviceBusyContent() string { return Text(mobileDeviceBusyKey) }

// MobileSelectorMissingText is what a mobile_* tool says when a selector
// argument (text/resource_id/content_desc/xpath) is required and none of them
// were given.
func MobileSelectorMissingText() string { return Text(mobileSelectorMissingKey) }

// MobileTapSelectorMissingText is mobile_tap's own version of
// MobileSelectorMissingText: a tap may also give raw coordinates, so it names
// that fourth way out too.
func MobileTapSelectorMissingText() string { return Text(mobileTapSelectorMissingKey) }

// MobileLaunchNoPackageText explains that a deploy target has no app package
// recorded for mobile_launch_app to install.
func MobileLaunchNoPackageText(env string) string {
	return mobileLaunchNoPackageKey.Render(mobileLaunchNoPackageInput{Env: env})
}

// MobileWaitForTimeoutText is mobile_wait_for's refusal once its deadline
// passes without the selector appearing.
func MobileWaitForTimeoutText(describe string, timeoutSeconds int) string {
	return mobileWaitForTimeoutKey.Render(mobileWaitForTimeoutInput{Describe: describe, TimeoutSeconds: timeoutSeconds})
}

// MobileScreenshotTooLargeText is mobile_screenshot's refusal when the device
// answered with more bytes than the model context should carry.
func MobileScreenshotTooLargeText(bytes int) string {
	return mobileScreenshotTooLargeKey.Render(mobileScreenshotTooLargeInput{Bytes: bytes})
}

// MobileLaunchHoldDeviceNote is appended to a successful mobile_launch_app
// result, reminding the agent it now holds an exclusive lease on the device.
func MobileLaunchHoldDeviceNote() string { return Text(mobileLaunchHoldDeviceKey) }

type mobileHubUnavailableInput struct{ Detail string }

var mobileHubUnavailableKey = Define("guard.mobile_hub_unavailable", mobileHubUnavailableInput{
	Detail: "appium exited before it answered on http://127.0.0.1:4723 (exit status 1)",
})

// MobileHubUnavailableText is what every mobile_* tool says when the Appium
// hub this server starts on demand would not start; detail is why.
func MobileHubUnavailableText(detail string) string {
	return mobileHubUnavailableKey.Render(mobileHubUnavailableInput{Detail: detail})
}
