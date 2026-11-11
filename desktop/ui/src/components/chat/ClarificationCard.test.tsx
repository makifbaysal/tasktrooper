import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ClarificationRequest } from "@/api";
import { ClarificationCard } from "@/components/chat/ClarificationCard";
import { I18nProvider } from "@/hooks/useI18n";

const request: ClarificationRequest = {
  context: "Guided audience setup",
  questions: [
    {
      id: "segments",
      prompt: "Which of these people do you want to talk to?",
      allow_multiple: true,
      options: [
        { id: "all", label: "All" },
        { id: "founders", label: "Founders" },
        { id: "other", label: "Other" },
      ],
    },
    {
      id: "countries",
      prompt: "Where are these people?",
      allow_multiple: true,
      max_selections: 2,
      options: [
        { id: "us", label: "US" },
        { id: "uk", label: "UK" },
        { id: "ca", label: "Canada" },
        { id: "other", label: "Other" },
      ],
    },
  ],
};

function setup() {
  const onSubmit = vi.fn();
  render(
    <I18nProvider>
      <ClarificationCard clarification={request} onSubmit={onSubmit} />
    </I18nProvider>,
  );
  return { onSubmit };
}

describe("ClarificationCard multi-select", () => {
  it("walks through two questions and submits every pick", () => {
    const { onSubmit } = setup();
    expect(screen.getByText("Question 1 of 2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    fireEvent.click(screen.getByRole("button", { name: "Founders" }));
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    expect(screen.getByText("Question 2 of 2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "US" }));
    fireEvent.click(screen.getByRole("button", { name: "UK" }));
    fireEvent.click(screen.getByRole("button", { name: /Submit/ }));
    expect(onSubmit).toHaveBeenCalledWith(
      "Which of these people do you want to talk to?: All, Founders\nWhere are these people?: US, UK",
    );
  });

  it("stops at max_selections and frees a slot when one is unpicked", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    expect(screen.getByText("Select up to 2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "US" }));
    fireEvent.click(screen.getByRole("button", { name: "UK" }));
    expect(screen.getByRole("button", { name: "Canada" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Other" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "UK" }));
    expect(screen.getByRole("button", { name: "Canada" })).toBeEnabled();
  });

  it("lets the user type their own answer", () => {
    const { onSubmit } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Other" }));
    fireEvent.change(screen.getByPlaceholderText(/Type your answer/), {
      target: { value: "Agencies" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    fireEvent.click(screen.getByRole("button", { name: "Canada" }));
    fireEvent.click(screen.getByRole("button", { name: /Submit/ }));
    expect(onSubmit).toHaveBeenCalledWith(
      "Which of these people do you want to talk to?: Agencies\nWhere are these people?: Canada",
    );
  });
});
