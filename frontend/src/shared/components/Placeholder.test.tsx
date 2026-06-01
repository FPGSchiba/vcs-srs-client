import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";

import { Placeholder } from "./Placeholder";

describe("Placeholder", () => {
  it("renders the provided label", () => {
    render(<Placeholder label="VCS · main window · tokens ready" />);
    expect(
      screen.getByText("VCS · main window · tokens ready"),
    ).toBeInTheDocument();
  });

  it("renders a distinct label per window", () => {
    render(<Placeholder label="VCS · comms popout · placeholder" />);
    expect(
      screen.getByText("VCS · comms popout · placeholder"),
    ).toBeInTheDocument();
  });
});
