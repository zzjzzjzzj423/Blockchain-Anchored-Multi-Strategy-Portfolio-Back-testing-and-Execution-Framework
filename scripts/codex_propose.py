#!/usr/bin/env python3
"""Ask Codex app-server for one development-only parameter proposal.

The Go caller owns candidate validation, evaluation, ranking, and artifacts. This
bridge receives only aggregate development-fold feedback on stdin and prints a
single structured proposal on stdout. It never reads the market-data checkout.
"""

import argparse
import json
import os
import queue
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path


SCHEMA = {
    "type": "object",
    "properties": {
        "parameters": {
            "type": "object",
            "properties": {
                "trend": {
                    "type": "object",
                    "properties": {
                        "fast_window": {"type": "integer", "enum": [10, 20, 30]},
                        "slow_window": {"type": "integer", "enum": [60, 100, 150]},
                    },
                    "required": ["fast_window", "slow_window"],
                    "additionalProperties": False,
                },
                "mean_reversion": {
                    "type": "object",
                    "properties": {
                        "lookback": {"type": "integer", "enum": [10, 20, 30]},
                        "entry_z": {"type": "number", "enum": [-0.5, -1.0, -1.5]},
                        "exit_z": {"type": "number", "enum": [0.0, -0.25]},
                    },
                    "required": ["lookback", "entry_z", "exit_z"],
                    "additionalProperties": False,
                },
                "allocator": {
                    "type": "object",
                    "properties": {
                        "reliability_window": {"type": "integer", "enum": [30, 63, 90]},
                        "volatility_window": {"type": "integer", "enum": [20, 30, 60]},
                    },
                    "required": ["reliability_window", "volatility_window"],
                    "additionalProperties": False,
                },
            },
            "required": ["trend", "mean_reversion", "allocator"],
            "additionalProperties": False,
        },
        "rationale": {"type": "string"},
    },
    "required": ["parameters", "rationale"],
    "additionalProperties": False,
}


def prompt_for(request):
    if request.get("schema_version") != "1.0.0" or not isinstance(request.get("history"), list):
        raise ValueError("expected schema_version 1.0.0 and a history array")
    history = request["history"]
    if not history or len(history) > 20:
        raise ValueError("expected 1–20 development assessments")
    return (
        "You are the proposal component of a bounded, development-only portfolio "
        "parameter search. Propose exactly one valid candidate not already in the "
        "history. The Go evaluator, not you, will run and rank it. Use only the "
        "development-fold aggregate feedback supplied below. Do not call tools, "
        "read files, browse, or infer anything from the final holdout. The frozen "
        "selection order is median validation net Sharpe, then worst-fold maximum "
        "drawdown, then median annualized turnover. Favor a useful unexplored "
        "candidate over claiming global optimality. Return the JSON object only.\n\n"
        "Development feedback (JSON):\n"
        + json.dumps(history, sort_keys=True, separators=(",", ":"))
    )


class AppServer:
    def __init__(self, codex_bin, cwd, timeout):
        self.timeout = timeout
        self.deadline = time.monotonic() + timeout
        self.proc = subprocess.Popen(
            [codex_bin, "app-server", "--listen", "stdio://"],
            cwd=cwd,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            bufsize=1,
        )
        self.lines = queue.Queue()
        self.reader = threading.Thread(target=self._read_lines, daemon=True)
        self.reader.start()
        self.next_id = 0

    def _read_lines(self):
        for line in self.proc.stdout:
            self.lines.put(line)
        self.lines.put(None)

    def send(self, method, params=None, request_id=None):
        message = {"method": method, "params": params or {}}
        if request_id is not None:
            message["id"] = request_id
        self.proc.stdin.write(json.dumps(message, separators=(",", ":")) + "\n")
        self.proc.stdin.flush()

    def receive(self):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("Codex app-server proposal timed out")
        try:
            line = self.lines.get(timeout=remaining)
        except queue.Empty as exc:
            raise TimeoutError("Codex app-server proposal timed out") from exc
        if line is None:
            raise RuntimeError("Codex app-server closed its output")
        message = json.loads(line)
        if "id" in message and "method" in message:
            # A proposal has no reason to execute tools or ask for permissions.
            raise RuntimeError("Codex requested an unexpected interactive action")
        return message

    def call(self, method, params=None):
        self.next_id += 1
        request_id = self.next_id
        self.send(method, params, request_id)
        while True:
            message = self.receive()
            if message.get("id") == request_id:
                if "error" in message:
                    raise RuntimeError(f"Codex {method} failed: {message['error'].get('message', 'unknown error')}")
                return message.get("result", {})

    def close(self):
        if self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=2)


def propose(request, codex_bin, model, timeout):
    prompt = prompt_for(request)
    with tempfile.TemporaryDirectory(prefix="msbd5017-codex-proposer-") as temporary:
        server = AppServer(codex_bin, temporary, timeout)
        try:
            server.call(
                "initialize",
                {"clientInfo": {"name": "msbd5017_proposer", "title": "MSBD5017 parameter proposer", "version": "1.0.0"}},
            )
            server.send("initialized")
            thread_params = {"cwd": temporary, "approvalPolicy": "never", "sandbox": "read-only"}
            if model:
                thread_params["model"] = model
            result = server.call("thread/start", thread_params)
            thread_id = result.get("thread", {}).get("id")
            if not thread_id:
                raise RuntimeError("Codex did not return a thread id")
            request_id = server.next_id + 1
            server.next_id = request_id
            server.send(
                "turn/start",
                {
                    "threadId": thread_id,
                    "input": [{"type": "text", "text": prompt}],
                    "cwd": temporary,
                    "approvalPolicy": "never",
                    "sandboxPolicy": {"type": "readOnly"},
                    "outputSchema": SCHEMA,
                },
                request_id,
            )
            completed_text = None
            turn_accepted = False
            while True:
                message = server.receive()
                if message.get("id") == request_id:
                    if "error" in message:
                        raise RuntimeError(f"Codex turn failed: {message['error'].get('message', 'unknown error')}")
                    turn_accepted = True
                if message.get("method") in {"item/started", "item/completed"}:
                    item = message.get("params", {}).get("item", {})
                    if item.get("type") == "agentMessage" and message.get("method") == "item/completed":
                        completed_text = item.get("text")
                    elif item.get("type") in {"commandExecution", "fileChange", "webSearch"}:
                        raise RuntimeError("Codex attempted a tool action during parameter proposal")
                if message.get("method") == "turn/completed":
                    turn = message.get("params", {}).get("turn", {})
                    if turn.get("status") != "completed":
                        raise RuntimeError(f"Codex turn ended with {turn.get('status')}: {turn.get('error')}")
                    if not turn_accepted or not completed_text:
                        raise RuntimeError("Codex completed without a structured proposal")
                    return json.loads(completed_text)
        finally:
            server.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--codex-bin", default=os.environ.get("CODEX_BIN", "codex"))
    parser.add_argument("--model", default="")
    parser.add_argument("--timeout", type=int, default=120)
    args = parser.parse_args()
    if args.timeout < 10 or args.timeout > 600:
        parser.error("timeout must be between 10 and 600 seconds")
    try:
        request = json.load(sys.stdin)
        result = propose(request, args.codex_bin, args.model, args.timeout)
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    except (ValueError, RuntimeError, TimeoutError, OSError, json.JSONDecodeError) as exc:
        print(f"Codex proposal failed: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc


if __name__ == "__main__":
    main()
