import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { TagInput } from "./tag-input";

function Harness({ initial = [] as string[] }) {
  const [values, setValues] = useState(initial);
  return (
    <>
      <TagInput id="types" label="Event types" values={values} onChange={setValues} />
      <output data-testid="values">{values.join("|")}</output>
    </>
  );
}

describe("TagInput", () => {
  it("commits what was typed on Enter, on a comma and when the field is left", () => {
    render(<Harness />);
    const input = screen.getByLabelText("Event types");
    fireEvent.change(input, { target: { value: "order.shipped" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.change(input, { target: { value: "order.paid" } });
    fireEvent.keyDown(input, { key: "," });
    fireEvent.change(input, { target: { value: "refund.issued" } });
    fireEvent.blur(input);
    expect(screen.getByTestId("values")).toHaveTextContent("order.shipped|order.paid|refund.issued");
    expect(input).toHaveValue("");
  });

  it("splits a pasted list and drops duplicates", () => {
    render(<Harness initial={["order.paid"]} />);
    const input = screen.getByLabelText("Event types");
    fireEvent.change(input, { target: { value: "order.paid, order.shipped order.shipped" } });
    fireEvent.blur(input);
    expect(screen.getByTestId("values")).toHaveTextContent("order.paid|order.shipped");
  });

  it("removes a chip with its button and the last one with Backspace", () => {
    render(<Harness initial={["a", "b", "c"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove a" }));
    expect(screen.getByTestId("values")).toHaveTextContent("b|c");
    fireEvent.keyDown(screen.getByLabelText("Event types"), { key: "Backspace" });
    expect(screen.getByTestId("values")).toHaveTextContent("b");
  });
});
