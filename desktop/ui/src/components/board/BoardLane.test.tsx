import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { BoardLane, type BoardLaneStage } from "@/components/board/BoardLane";

const stage: BoardLaneStage = { slug: "todo", label: "Todo", count: 1 };

function Harness() {
  const [dropColumn, setDropColumn] = useState<string | null>(null);
  return (
    <BoardLane
      stages={[stage]}
      dragging
      dropColumn={dropColumn}
      onDropColumnChange={setDropColumn}
      onDropTask={() => {}}
      renderStage={() => <div>card</div>}
    />
  );
}

describe("BoardLane drag-over highlight", () => {
  it("highlights the lane against the primary hue once a dragged card is over it", () => {
    render(<Harness />);
    const lane = screen.getByText("Todo").closest("[data-column-slug='todo']") as HTMLElement;

    expect(lane.className).not.toContain("border-primary");

    fireEvent.dragOver(lane);

    expect(lane.className).toContain("border-primary");
    expect(lane.className).toContain("bg-primary/5");
    expect(lane.className).toContain("ring-2");
    expect(lane.className).toContain("ring-primary/20");
  });

  it("clears the highlight once the drag leaves the lane", () => {
    render(<Harness />);
    const lane = screen.getByText("Todo").closest("[data-column-slug='todo']") as HTMLElement;

    fireEvent.dragOver(lane);
    expect(lane.className).toContain("border-primary");

    fireEvent.dragLeave(lane, { relatedTarget: document.body });

    expect(lane.className).not.toContain("border-primary");
  });
});
