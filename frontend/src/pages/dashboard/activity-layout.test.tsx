import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { ActivityLayout } from "./activity-layout";

vi.mock("@/components/activity/activity-list", () => ({
  ActivityList: () => <div data-testid="activity-list" />,
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function renderPage(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/activity" element={<ActivityLayout />}>
          <Route path=":messageId" element={<div data-testid="detail" />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe("activity-layout", () => {
  it("shows the list and the empty hint when no activity is selected", () => {
    renderPage("/activity");

    expect(screen.getByTestId("activity-list")).toBeInTheDocument();
    expect(screen.getByText("activity.empty-hint")).toBeInTheDocument();
  });

  it("renders the selected activity in the right pane", () => {
    renderPage("/activity/msg1");

    expect(screen.getByTestId("activity-list")).toBeInTheDocument();
    expect(screen.getByTestId("detail")).toBeInTheDocument();
    expect(screen.queryByText("activity.empty-hint")).not.toBeInTheDocument();
  });
});
