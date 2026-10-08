import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { ProtectedRoute } from "@/components/layout/ProtectedRoute";

vi.mock("@/hooks/useSetup", () => ({
  useSetup: () => ({ redirectToSetup: true, deciding: false }),
}));

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<ProtectedRoute />}>
          <Route path="/setup" element={<p>setup</p>} />
          <Route path="/board" element={<p>board</p>} />
          <Route path="/projects/new" element={<p>add repository</p>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("ProtectedRoute", () => {
  it("sends an unfinished setup back to /setup", () => {
    renderAt("/board");
    expect(screen.getByText("setup")).toBeInTheDocument();
  });

  it("lets the setup's own add-repository link through", () => {
    renderAt("/projects/new?project=proj-1");
    expect(screen.getByText("add repository")).toBeInTheDocument();
  });
});
