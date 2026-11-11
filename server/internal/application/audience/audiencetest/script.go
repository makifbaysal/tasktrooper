package audiencetest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var (
	countRe = regexp.MustCompile(`Create exactly (\d+) new people`)
	segRe   = regexp.MustCompile(`(?m)^- ([^:\n]+): (\d+)`)
	nameRe  = regexp.MustCompile(`Who you are: ([^,.\n]+)`)
)

// ScriptedModel invents numbered people for the segments it is told about,
// answers every interview from a table by name and writes a fixed synthesis.
func ScriptedModel(answerFor func(name string) string) *Model {
	m := &Model{}
	var n int
	m.On("You invent the synthetic people", func(req domain.AgentRequest) (string, error) {
		user := req.Messages[len(req.Messages)-1].Content
		count, _ := strconv.Atoi(countRe.FindStringSubmatch(user)[1])
		var segs []string
		for _, sm := range segRe.FindAllStringSubmatch(user, -1) {
			c, _ := strconv.Atoi(sm[2])
			for i := 0; i < c; i++ {
				segs = append(segs, sm[1])
			}
		}
		var people []map[string]any
		for i := 0; i < count; i++ {
			n++
			seg := ""
			if i < len(segs) {
				seg = segs[i]
			}
			people = append(people, map[string]any{"name": fmt.Sprintf("Person %d", n), "segment": seg, "age": 30 + n, "role": "Designer", "location": "Berlin",
				"bio": "Works alone.", "goals": []string{"more clients"}, "pains": "too many tools", "budget": "tight", "tech_savviness": "high", "quote": "I hate tool sprawl."})
		}
		raw, _ := json.Marshal(map[string]any{"personas": people})
		return "Here you go:\n```json\n" + string(raw) + "\n```", nil
	})
	m.On("You are a character in a market-research interview", func(req domain.AgentRequest) (string, error) {
		return answerFor(nameRe.FindStringSubmatch(req.Messages[0].Content)[1]), nil
	})
	m.Say("You design the interview script", `{"sections":[{"title":"Today","questions":["How do you handle this today?","What went wrong last time?"]},{"title":"Decision","questions":["What would make you switch?"]}]}`)
	m.Say("You are a market-research analyst", `{"summary":"Interest is lukewarm and price-bound.","verdict_rationale":"Mean intent is middling.",
	  "segment_insights":[{"segment":"Freelancers","insight":"Freelancers balk at the price."}],
	  "objection_themes":[{"label":"Too expensive","personas":["p1","p2","P3","p99"]}],
	  "need_themes":[{"label":"Invoicing","personas":["p1"]}],
	  "alternative_themes":[{"label":"Spreadsheets","personas":["p2","p3"]}],
	  "quotes":[{"persona":"p1","text":"too pricey for me","topic":"price","sentiment":"negative"},{"persona":"p2","text":"I made this up","topic":"x","sentiment":"positive"}],
	  "recommendations":[{"title":"Add a free tier","detail":"Lower the barrier.","priority":"high"}],
	  "open_questions":[{"question":"What would convince you?","digest":"A free tier."}]}`)
	m.Say("You answer the product owner's questions", "Person 1 objected most, saying \"too pricey for me\".")
	m.On("You are playing one person", func(req domain.AgentRequest) (string, error) {
		return "As I said, it depends on the price.", nil
	})
	return m
}

func AnswerJSON(score int, verdict string, maxPrice float64, buyAt9 bool) string {
	return fmt.Sprintf(`{"intent_score":%d,"verdict":"%s","would_pay_now":%t,"reasoning":"too pricey for me, honestly","max_price":%v,
	"price_reactions":[{"price":9,"would_buy":%t,"comment":"ok"},{"price":99,"would_buy":false},{"price":5,"would_buy":true}],
	"missing":["Invoicing"],"objections":["Too expensive","too expensive"],"alternatives":["Spreadsheets"],"quote":"I hate tool sprawl.",
	"answers":[{"question":"What would convince you?","answer":"A free tier."}]}`, score, verdict, verdict == "yes", maxPrice, buyAt9)
}
