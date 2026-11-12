package audience

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

func CompareThemesForTest(before, after []domain.Theme) []ThemeChange {
	return compareThemes(before, after)
}
