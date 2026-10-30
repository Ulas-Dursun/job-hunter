package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// TechnicalQA represents a targeted technical interview question and answer guide.
type TechnicalQA struct {
	Question       string `json:"question"`
	WhyTheyAsk     string `json:"why_they_ask"`
	AnswerStrategy string `json:"answer_strategy"`
}

// BehavioralSTAR represents a behavioral interview question with a tailored STAR response script.
type BehavioralSTAR struct {
	Question       string `json:"question"`
	STARStoryGuide string `json:"star_story_guide"`
}

// InterviewPrepAssets represents the structured AI Interview Prep kit for a specific job.
type InterviewPrepAssets struct {
	RoleFocusSummary    string           `json:"role_focus_summary"`
	TechnicalQuestions  []TechnicalQA    `json:"technical_questions"`
	BehavioralQuestions []BehavioralSTAR `json:"behavioral_questions"`
	QuestionsToAsk      []string         `json:"questions_to_ask"`
}

// GenerateInterviewPrep produces 3 technical questions, 2 STAR behavioral guides, and 2 reverse interview questions.
func (s *TailorService) GenerateInterviewPrep(
	ctx context.Context,
	title, company, location, description string,
) (*InterviewPrepAssets, error) {
	prompt := s.buildInterviewPrompt(title, company, location, description)
	rawText, err := s.client.Generate(ctx, prompt, 3500)
	if err != nil {
		return nil, err
	}
	return parseInterviewResult(rawText)
}

func (s *TailorService) buildInterviewPrompt(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)
	if desc == "" {
		desc = "(No detailed description provided — generate based on job title, company, and candidate profile.)"
	}

	return fmt.Sprintf(`You are a Staff Engineer and Hiring Manager at %s interviewing %s for the %s position (%s).
Create a concise, high-signal Interview Preparation Kit connecting %s's domain with the candidate's actual projects and experience.

REQUIREMENTS:
1. role_focus_summary:
   - 2 concise sentences in Turkish summarizing what %s cares about most for this role and which 1-2 of the candidate's real experiences or projects should be the centerpiece.
2. technical_questions (EXACTLY 3 items — quality over quantity):
   - Pick the 3 most likely technical / system-design questions for THIS specific role.
   - "question": The exact question the interviewer will ask (in English, with brief Turkish context if helpful).
   - "why_they_ask": 1 short sentence in Turkish explaining what they are testing.
   - "answer_strategy": 2-3 punchy sentences in Turkish (keeping technical terms in English) showing how the candidate should answer using real metrics and trade-offs from the Master Profile — no long essays.
3. behavioral_questions (EXACTLY 2 items):
   - "question": The behavioral question in English.
   - "star_story_guide": Concise 3-sentence STAR (Situation/Task, Action, Result) guide in Turkish based on the candidate's real experience in the Master Profile.
4. questions_to_ask (EXACTLY 2 items):
   - 2 sharp engineering questions (in English, with a 1-line Turkish note on why it works) for the end of the interview.

CANDIDATE MASTER PROFILE:
%s

TARGET JOB POSTING:
Title: %s
Company: %s
Location: %s
Description:
%s

OUTPUT FORMAT:
Respond with a strict, valid JSON object without markdown formatting, backticks, or preamble:
{
  "role_focus_summary": "string",
  "technical_questions": [
    {
      "question": "string",
      "why_they_ask": "string",
      "answer_strategy": "string"
    }
  ],
  "behavioral_questions": [
    {
      "question": "string",
      "star_story_guide": "string"
    }
  ],
  "questions_to_ask": ["string", "string"]
}`, company, s.candidateName, title, location, company, company, s.masterProfile, title, company, location, desc)
}

func parseInterviewResult(text string) (*InterviewPrepAssets, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("no valid JSON found in interview prep response")
	}

	var result InterviewPrepAssets
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		slog.Error("failed to unmarshal interview prep json", "raw", text[start:end+1])
		return nil, fmt.Errorf("unmarshaling interview prep json: %w", err)
	}

	if result.TechnicalQuestions == nil {
		result.TechnicalQuestions = []TechnicalQA{}
	}
	if result.BehavioralQuestions == nil {
		result.BehavioralQuestions = []BehavioralSTAR{}
	}
	if result.QuestionsToAsk == nil {
		result.QuestionsToAsk = []string{}
	}

	return &result, nil
}
