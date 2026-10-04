import { EventEmitter } from "node:events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const spawnMock = vi.fn();
const execFileMock = vi.fn();
vi.mock("node:child_process", () => ({ spawn: spawnMock, execFile: execFileMock }));

const { SupervisedChild } = await import("./child.js");

class FakeProc extends EventEmitter {
  pid = 4242;
  exitCode: number | null = null;
  killed = false;
  stdout = null;
  stderr = null;
  stdin = { end: vi.fn(), on: vi.fn() };
  kill = vi.fn();
  exit(): void {
    this.exitCode = 0;
    this.emit("exit", 0, null);
  }
}

const realPlatform = process.platform;
function setPlatform(p: NodeJS.Platform): void {
  Object.defineProperty(process, "platform", { value: p });
}

async function started(stdinPipe: boolean): Promise<{ child: InstanceType<typeof SupervisedChild>; proc: FakeProc }> {
  const proc = new FakeProc();
  spawnMock.mockReturnValue(proc);
  const child = new SupervisedChild({ id: "embedder", command: "node", args: ["x"], env: {}, restart: false, stdinPipe });
  const p = child.start();
  proc.emit("spawn");
  await p;
  return { child, proc };
}

describe("SupervisedChild.stop", () => {
  let killSpy: ReturnType<typeof vi.spyOn>;
  beforeEach(() => {
    spawnMock.mockReset();
    execFileMock.mockReset();
    execFileMock.mockImplementation((_f, _a, _o, cb: () => void) => cb());
    killSpy = vi.spyOn(process, "kill").mockImplementation(() => true);
  });
  afterEach(() => {
    setPlatform(realPlatform);
    killSpy.mockRestore();
    vi.useRealTimers();
  });

  it("signals the whole process group on POSIX", async () => {
    setPlatform("linux");
    const { child, proc } = await started(true);
    killSpy.mockImplementation(() => {
      proc.exit();
      return true;
    });
    await child.stop();
    expect(killSpy).toHaveBeenCalledWith(-4242, "SIGTERM");
    expect(proc.stdin.end).toHaveBeenCalled();
  });

  it("falls back to the pid when the group does not exist", async () => {
    setPlatform("linux");
    const { child, proc } = await started(true);
    killSpy.mockImplementation((pid: number) => {
      if (pid < 0) throw Object.assign(new Error("no group"), { code: "ESRCH" });
      proc.exit();
      return true;
    });
    await child.stop();
    expect(killSpy).toHaveBeenCalledWith(4242, "SIGTERM");
  });

  it("escalates to SIGKILL on the group after the grace period", async () => {
    setPlatform("linux");
    vi.useFakeTimers();
    const { child, proc } = await started(true);
    const stopped = child.stop();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(killSpy).toHaveBeenCalledWith(-4242, "SIGKILL");
    proc.exit();
    await stopped;
  });

  it("uses taskkill /T /F straight away for a Windows child without a stdin pipe", async () => {
    setPlatform("win32");
    const { child, proc } = await started(false);
    execFileMock.mockImplementation((_f, _a, _o, cb: () => void) => {
      proc.exit();
      cb();
    });
    await child.stop();
    expect(execFileMock).toHaveBeenCalledWith(
      "taskkill",
      ["/T", "/F", "/PID", "4242"],
      { windowsHide: true },
      expect.any(Function),
    );
    expect(proc.kill).not.toHaveBeenCalled();
  });

  it("gives a Windows child with a stdin pipe the grace period before taskkill", async () => {
    setPlatform("win32");
    vi.useFakeTimers();
    const { child, proc } = await started(true);
    execFileMock.mockImplementation((_f, _a, _o, cb: () => void) => {
      proc.exit();
      cb();
    });
    const stopped = child.stop();
    await vi.advanceTimersByTimeAsync(29_000);
    expect(execFileMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1_000);
    await stopped;
    expect(execFileMock).toHaveBeenCalledTimes(1);
  });
});
