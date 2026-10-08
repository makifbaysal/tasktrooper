package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/toolattach"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const screenshotToolName = "browser_screenshot"

// maxBase64Bytes is what a screenshot may occupy in the model context as
// base64. Above it the shot is retaken as JPEG at reduced quality rather than
// failing — a blurry screenshot still answers "does the page render".
const maxBase64Bytes = 1536 * 1024

const jpegFallbackQuality = 70

type screenshotArgs struct {
	FullPage     bool   `json:"full_page"`
	Width        int64  `json:"width"`
	AttachToTask bool   `json:"attach_to_task"`
	Title        string `json:"title"`
}

type screenshotTool struct {
	session *Session
}

func newScreenshotTool(session *Session) port.ToolExecutor {
	return &screenshotTool{session: session}
}

func (t *screenshotTool) Name() string {
	return screenshotToolName
}

func (t *screenshotTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: screenshotToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"full_page": map[string]interface{}{
						"type": "boolean",
					},
					"width": map[string]interface{}{
						"type": "integer",
					},
					"attach_to_task": map[string]interface{}{
						"type": "boolean",
					},
					"title": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

// captureJPEGViewport is the oversize fallback for viewport shots;
// chromedp.CaptureScreenshot has no quality knob, so it goes to CDP directly.
func captureJPEGViewport(buf *[]byte) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		data, err := page.CaptureScreenshot().
			WithFormat(page.CaptureScreenshotFormatJpeg).
			WithQuality(jpegFallbackQuality).
			Do(ctx)
		if err != nil {
			return err
		}
		*buf = data
		return nil
	})
}

func (t *screenshotTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a screenshotArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(screenshotToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	var buf []byte
	capture := chromedp.Action(chromedp.CaptureScreenshot(&buf))
	if a.FullPage {
		capture = chromedp.FullScreenshot(&buf, 100)
	}

	var actions []chromedp.Action
	// Only an explicit width re-emulates. It used to do so unconditionally, at
	// the desktop default, which silently threw away whatever browser_set_viewport
	// had put the tab in: a phone check became a 1440px check the moment it was
	// photographed.
	if a.Width > 0 {
		// 844 is the common mobile logical height (390x844); anything wider gets
		// the desktop default.
		height := int64(defaultViewportHeight)
		if a.Width < 800 {
			height = 844
		}
		actions = append(actions, chromedp.EmulateViewport(a.Width, height))
	}
	actions = append(actions, capture)
	// The picture alone does not say which page it is of, or at what size: a
	// blank tab looks exactly like a broken product, and a mobile shot that
	// silently came back desktop-sized is a responsive check that proved nothing.
	// Measured from the page rather than from the arguments, so it reports what
	// was actually captured.
	// Broken images are measured from the DOM, not from the pixels: a failed
	// <img> renders as a small "?" placeholder the vision pass routinely
	// glosses over — a broken store badge passed both the developer and QA as
	// "looks correct". naturalWidth===0 on a complete image is the browser's
	// own verdict that the asset did not load, and text in the tool result is
	// something the model cannot claim it did not see.
	var raw string
	actions = append(actions, chromedp.Evaluate(
		`JSON.stringify({url: location.href, title: document.title, `+
			`width: document.documentElement.clientWidth, height: document.documentElement.clientHeight, `+
			`brokenImages: Array.from(document.images).filter(function(i){return i.complete && i.naturalWidth === 0;})`+
			`.map(function(i){return (i.getAttribute('src') || i.currentSrc || '(no src)').slice(0, 200);}).slice(0, 10), `+
			`loadingImages: Array.from(document.images).filter(function(i){return !i.complete;}).length})`, &raw))

	err := t.session.run(ctx, executeTimeout, actions...)
	if err != nil {
		return runError(screenshotToolName, "screenshot", err)
	}

	var shot struct {
		URL           string   `json:"url"`
		Title         string   `json:"title"`
		Width         int      `json:"width"`
		Height        int      `json:"height"`
		BrokenImages  []string `json:"brokenImages"`
		LoadingImages int      `json:"loadingImages"`
	}
	if err := json.Unmarshal([]byte(raw), &shot); err != nil {
		return toolError(screenshotToolName, fmt.Sprintf("decode page state: %v", err))
	}

	mediaType := "image/png"
	encoded := base64.StdEncoding.EncodeToString(buf)
	if len(encoded) > maxBase64Bytes {
		retake := captureJPEGViewport(&buf)
		if a.FullPage {
			retake = chromedp.FullScreenshot(&buf, jpegFallbackQuality)
		}
		if err := t.session.run(ctx, executeTimeout, retake); err != nil {
			return runError(screenshotToolName, "screenshot (jpeg retake)", err)
		}
		mediaType = "image/jpeg"
		encoded = base64.StdEncoding.EncodeToString(buf)
	}

	content := fmt.Sprintf("screenshot attached: %s, viewport %dx%d, full_page=%v, %d bytes\nurl: %s\ntitle: %s",
		mediaType, shot.Width, shot.Height, a.FullPage, len(buf), shot.URL, shot.Title)
	if len(shot.BrokenImages) > 0 {
		content += prompt.BrowserScreenshotBrokenImagesText(len(shot.BrokenImages), strings.Join(shot.BrokenImages, "\n- "))
	}
	if shot.LoadingImages > 0 {
		content += prompt.BrowserScreenshotLoadingImagesText(shot.LoadingImages)
	}
	image := domain.ToolResultImage{MediaType: mediaType, Data: encoded}
	if a.AttachToTask {
		content += toolattach.Note(ctx, t.session.taskAttacher(), screenshotToolName, a.Title, image)
	}
	return domain.ToolResult{
		Name:    screenshotToolName,
		Content: content,
		IsError: false,
		Images:  []domain.ToolResultImage{image},
	}
}
