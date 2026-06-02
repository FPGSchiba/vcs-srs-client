import { describe, it, expect, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { useClients } from "../../../shared/store/clients";
import { Players } from "./Players";

describe("Players", () => {
  beforeEach(() => useClients.setState({ clients: {} }));

  it("renders a row per connected client", () => {
    useClients.setState({
      clients: {
        g1: { name: "Alice", coalition: "VG", unit_id: "DSC", role_id: 1 },
        g2: { name: "Bob", coalition: "VG", unit_id: "SHN", role_id: 0 },
      },
    });
    render(<Players />);
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("Bob")).toBeInTheDocument();
  });
});
