import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EvidenceList, evidenceLabel } from "@/components/projects/model/EvidenceList";
import { I18nProvider } from "@/hooks/useI18n";

describe("evidenceLabel", () => {
  it("appends the line number when present", () => {
    expect(evidenceLabel({ path: "cmd/api/main.go", line: 42 })).toBe("cmd/api/main.go:42");
  });
  it("is just the path when there is no line", () => {
    expect(evidenceLabel({ path: "go.mod" })).toBe("go.mod");
  });
});

describe("EvidenceList", () => {
  it("renders nothing for an empty list", () => {
    const { container } = render(
      <I18nProvider>
        <EvidenceList evidence={[]} />
      </I18nProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders one entry per evidence item, with note text appended", () => {
    render(
      <I18nProvider>
        <EvidenceList
          evidence={[
            { path: "cmd/api/main.go", line: 42 },
            { path: "go.mod", note: "module declares the package name" },
          ]}
        />
      </I18nProvider>,
    );
    expect(screen.getByText("cmd/api/main.go:42")).toBeInTheDocument();
    expect(screen.getByText("go.mod")).toBeInTheDocument();
    expect(screen.getByText("— module declares the package name")).toBeInTheDocument();
  });
});
