import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useThrottledCommit } from "./useThrottledCommit";

describe("useThrottledCommit", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("leading call sends immediately", () => {
    const send = vi.fn();
    const { result } = renderHook(() => useThrottledCommit<number>(send, 150));
    act(() => result.current.push(1));
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith(1);
  });

  it("pushes inside the window coalesce into one trailing send of the last value", () => {
    const send = vi.fn();
    const { result } = renderHook(() => useThrottledCommit<number>(send, 150));
    act(() => result.current.push(1));
    act(() => {
      vi.advanceTimersByTime(20);
      result.current.push(2);
      vi.advanceTimersByTime(20);
      result.current.push(3);
      vi.advanceTimersByTime(20);
      result.current.push(4);
    });
    expect(send).toHaveBeenCalledTimes(1);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(send).toHaveBeenCalledTimes(2);
    expect(send).toHaveBeenLastCalledWith(4);
  });

  it("120 pushes over 2000ms send at most ceil(2000/150)+1 times", () => {
    const send = vi.fn();
    const { result } = renderHook(() => useThrottledCommit<number>(send, 150));
    act(() => {
      for (let i = 0; i < 120; i++) {
        result.current.push(i);
        vi.advanceTimersByTime(2000 / 120);
      }
      vi.advanceTimersByTime(300);
    });
    expect(send.mock.calls.length).toBeLessThanOrEqual(Math.ceil(2000 / 150) + 1);
    expect(send).toHaveBeenLastCalledWith(119);
  });

  it("flush sends now and cancels the trailing send", () => {
    const send = vi.fn();
    const { result } = renderHook(() => useThrottledCommit<number>(send, 150));
    act(() => result.current.push(1));
    act(() => result.current.push(2)); // queued as trailing
    act(() => result.current.flush(3));
    expect(send).toHaveBeenCalledTimes(2);
    expect(send).toHaveBeenLastCalledWith(3);
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(send).toHaveBeenCalledTimes(2);
  });

  it("unmount sends the pending value", () => {
    const send = vi.fn();
    const { result, unmount } = renderHook(() => useThrottledCommit<number>(send, 150));
    act(() => result.current.push(1));
    act(() => result.current.push(2));
    unmount();
    expect(send).toHaveBeenCalledTimes(2);
    expect(send).toHaveBeenLastCalledWith(2);
    vi.advanceTimersByTime(500);
    expect(send).toHaveBeenCalledTimes(2);
  });

  it("returns stable functions across renders", () => {
    const { result, rerender } = renderHook(() => useThrottledCommit<number>(vi.fn(), 150));
    const first = result.current;
    rerender();
    expect(result.current.push).toBe(first.push);
    expect(result.current.flush).toBe(first.flush);
  });
});
