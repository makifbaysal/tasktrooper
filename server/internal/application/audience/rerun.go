package audience

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type productChangesInput struct{ Changes string }

var productChangesKey = prompt.Define("audience.product_changes", productChangesInput{Changes: "The pricing page now shows the price."})

// RerunTitleSuffix marks a study that runs again after the product changed.
const RerunTitleSuffix = " (re-run)"

// Rerun plans a new study that asks the same people the same questions again
// after the product changed, so the two reports can be compared. It copies the
// finished study's kind, goal, prices and interview script (the script waits
// for the person's approval again, so nothing runs on its own) and carries the
// person's note of what changed. A page given by URL is read again when the
// new study starts.
func (s *Service) Rerun(ctx context.Context, studyID uuid.UUID, changes string) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, studyID)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDone || st.Report == nil {
		return domain.Study{}, invalid("only a study with a finished report can be run again")
	}
	inputs := st.Inputs
	inputs.Pages = append([]domain.PageInput(nil), st.Inputs.Pages...)
	for i := range inputs.Pages {
		if inputs.Pages[i].URL != "" {
			inputs.Pages[i].Copy = ""
		}
	}
	title := st.Title
	if len(title)+len(RerunTitleSuffix) <= 120 {
		title += RerunTitleSuffix
	}
	return s.PlanStudy(ctx, st.AudienceID, PlanStudyInput{
		Title: title, Goal: st.Goal, Questions: st.Questions, PricePoints: st.PricePoints, Currency: st.Currency,
		Kind: string(st.Kind), Inputs: inputs, Script: st.Script,
		ParentStudyID: &st.ID, Changes: clip(changes, maxFieldLen),
	})
}

// withChanges adds the person's note of what changed to the product text a
// re-run shows its people.
func withChanges(product, changes string) string {
	if changes == "" {
		return product
	}
	return product + "\n\n" + productChangesKey.Render(productChangesInput{Changes: changes})
}
