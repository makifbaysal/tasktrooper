import "@testing-library/jest-dom/vitest";
import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { EntityIndexRedirect, LegacyComponentRedirect, RedirectKeepingSearch, legacyComponentTarget } from "@/pages/ops/opsRedirects";

function Where() {
  const { pathname, search } = useLocation();
  return <p data-testid="where">{`${pathname}${search}`}</p>;
}

// The same redirect table App.tsx declares for the retired screens.
function renderAt(path: string) {
  const { getByTestId, unmount } = render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/operations">
          <Route path="deployments" element={<RedirectKeepingSearch to="/operations/changes" />} />
          <Route path="incidents" element={<RedirectKeepingSearch to="/operations/issues" />} />
          <Route path="apps" element={<RedirectKeepingSearch to="/operations/mobile" />} />
          <Route path="c/:componentId" element={<LegacyComponentRedirect />} />
          <Route path="entity/:componentId">
            <Route index element={<EntityIndexRedirect />} />
            <Route path="*" element={<Where />} />
          </Route>
          <Route path="*" element={<Where />} />
        </Route>
        <Route path="/incidents" element={<RedirectKeepingSearch to="/operations/issues" />} />
      </Routes>
    </MemoryRouter>,
  );
  const where = getByTestId("where").textContent;
  unmount();
  return where;
}

describe("retired Operations addresses", () => {
  it("sends Deployments to Changes and Mobile Apps to Mobile", () => {
    expect(renderAt("/operations/deployments?env=staging")).toBe("/operations/changes?env=staging");
    expect(renderAt("/operations/apps")).toBe("/operations/mobile");
  });

  it("sends Incidents to Issues and keeps the incident it pointed at", () => {
    expect(renderAt("/operations/incidents?id=i-42")).toBe("/operations/issues?id=i-42");
    expect(renderAt("/incidents?id=i-7")).toBe("/operations/issues?id=i-7");
  });

  it("sends the old component page to the entity page, section and window included", () => {
    expect(renderAt("/operations/c/c-api")).toBe("/operations/entity/c-api/summary");
    expect(renderAt("/operations/c/c-api?env=staging&range=15m&tab=deploys")).toBe(
      "/operations/entity/c-api/changes?env=staging&range=30m",
    );
  });

  it("opens an entity on its Summary", () => {
    expect(renderAt("/operations/entity/c-api?range=7d")).toBe("/operations/entity/c-api/summary?range=7d");
  });
});

describe("legacyComponentTarget", () => {
  it("drops what the console no longer reads", () => {
    expect(legacyComponentTarget("c-1", "?env=production&range=1h&tab=logs")).toBe("/operations/entity/c-1/logs");
    expect(legacyComponentTarget("c-1", "?env=moon&range=2y&tab=nope")).toBe("/operations/entity/c-1/summary");
    expect(legacyComponentTarget("c-1", "?range=24h&tab=errors")).toBe("/operations/entity/c-1/errors?range=24h");
  });
});
