"""Simple debug client to validate backend event streaming."""

import asyncio
import json
import sys
from typing import Optional

import click
import httpx


async def run_debug_client(
    target: str,
    backend_url: str = "http://localhost:8000",
    scan_mode: str = "standard",
    instruction: str = "",
    mock_tools: bool = False,
    model: Optional[str] = None,
) -> None:
    """Connect to backend and stream events."""
    print(f"🔗 Connecting to backend at {backend_url}")

    async with httpx.AsyncClient() as client:
        # 1. Start scan
        print(f"\n📤 Starting scan...")
        print(f"   Target: {target}")
        print(f"   Scan mode: {scan_mode}")
        if instruction:
            print(f"   Instruction: {instruction[:60]}...")
        print(f"   Mock tools: {mock_tools}")

        try:
            response = await client.post(
                f"{backend_url}/scans",
                json={
                    "target": target,
                    "scan_mode": scan_mode,
                    "instruction": instruction,
                    "mock_tools": mock_tools,
                    "model": model,
                },
                timeout=10.0,
            )
            response.raise_for_status()
            scan_data = response.json()
            scan_id = scan_data["scan_id"]
            print(f"✅ Scan started: {scan_id}\n")

        except httpx.ConnectError:
            print(f"❌ Failed to connect to backend at {backend_url}")
            sys.exit(1)
        except Exception as e:
            print(f"❌ Failed to start scan: {e}")
            sys.exit(1)

        # 2. Stream events
        print("📡 Streaming events from backend...\n")
        event_count = 0
        try:
            async with client.stream(
                "GET",
                f"{backend_url}/scans/{scan_id}/events",
                timeout=None,
            ) as stream:
                if stream.status_code != 200:
                    print(f"❌ Failed to stream events: HTTP {stream.status_code}")
                    sys.exit(1)

                async for line in stream.aiter_lines():
                    if not line:
                        continue

                    try:
                        event = json.loads(line)
                        event_count += 1
                        event_type = event.get("type", "unknown")

                        # Format output based on event type
                        if event_type == "scan_started":
                            print(f"[{event_count}] 📊 SCAN_STARTED")
                            print(f"      Target: {event.get('target')}")
                            print(f"      Mode: {event.get('scan_mode')}")
                            print(f"      Model: {event.get('model')}")

                        elif event_type == "scan_configured":
                            print(f"[{event_count}] ⚙️  SCAN_CONFIGURED")
                            print(f"      Tools: {event.get('tools_count')}")
                            skills = event.get('skills', [])
                            if skills:
                                print(f"      Skills: {', '.join(skills)}")
                            print(f"      Sandbox: {event.get('sandbox_url')}")
                            print(f"      Mock tools: {event.get('mock_tools')}")

                        elif event_type == "agent_started":
                            print(f"[{event_count}] 🤖 AGENT_STARTED")
                            print(f"      Role: {event.get('role')}")
                            print(f"      Iteration: {event.get('iteration')}")

                        elif event_type == "agent_token_usage":
                            print(f"[{event_count}] 📊 TOKEN_USAGE")
                            print(f"      Role: {event.get('role')}")
                            print(f"      Input: {event.get('input_tokens')}")
                            print(f"      Output: {event.get('output_tokens')}")
                            cache_read = event.get('cache_read_tokens', 0)
                            cache_write = event.get('cache_write_tokens', 0)
                            if cache_read or cache_write:
                                print(f"      Cache read: {cache_read}, write: {cache_write}")

                        elif event_type == "agent_thinking":
                            thinking = event.get('thinking', '')
                            preview = thinking[:100] + "..." if len(thinking) > 100 else thinking
                            print(f"[{event_count}] 💭 AGENT_THINKING")
                            print(f"      Role: {event.get('role')}")
                            print(f"      Thinking: {preview}")

                        elif event_type == "agent_message":
                            message = event.get('message', '')
                            preview = message[:100] + "..." if len(message) > 100 else message
                            print(f"[{event_count}] 💬 AGENT_MESSAGE")
                            print(f"      Role: {event.get('role')}")
                            print(f"      Message: {preview}")

                        elif event_type == "tool_executed":
                            print(f"[{event_count}] 🔧 TOOL_EXECUTED")
                            print(f"      Tool: {event.get('tool_name')}")
                            if event.get('command'):
                                cmd = event['command'][:80] + "..." if len(event['command']) > 80 else event['command']
                                print(f"      Command: {cmd}")
                            if event.get('output'):
                                out = event['output'][:80] + "..." if len(event['output']) > 80 else event['output']
                                print(f"      Output: {out}")

                        elif event_type == "vulnerability_found":
                            print(f"[{event_count}] 🚨 VULNERABILITY_FOUND")
                            print(f"      Title: {event.get('title')}")
                            print(f"      Severity: {event.get('severity')}")
                            desc = event.get('description', '')[:80]
                            print(f"      Description: {desc}...")

                        elif event_type == "agent_completed":
                            print(f"[{event_count}] ✅ AGENT_COMPLETED")
                            print(f"      Role: {event.get('role')}")
                            print(f"      Vulnerabilities found: {event.get('vulnerabilities_found')}")

                        elif event_type == "log_message":
                            level = event.get('level', 'info')
                            msg = event.get('message', '')[:100]
                            print(f"[{event_count}] 📝 LOG ({level.upper()}): {msg}")

                        elif event_type == "scan_completed":
                            print(f"[{event_count}] 🏁 SCAN_COMPLETED")
                            print(f"      Duration: {event.get('duration_seconds'):.1f}s")
                            print(f"      Vulnerabilities: {event.get('vulnerabilities_count')}")
                            print(f"      Iterations: {event.get('iterations')}")

                        elif event_type == "scan_failed":
                            print(f"[{event_count}] ❌ SCAN_FAILED")
                            print(f"      Error: {event.get('error')}")

                        else:
                            print(f"[{event_count}] ❓ {event_type.upper()}")

                        print()

                    except json.JSONDecodeError:
                        print(f"❌ Failed to parse JSON: {line}")
                        continue

        except httpx.ConnectError:
            print(f"❌ Connection lost to backend")
            sys.exit(1)
        except Exception as e:
            print(f"❌ Error streaming events: {e}")
            sys.exit(1)

        print(f"\n✅ Stream ended ({event_count} events total)")


@click.command()
@click.option("--target", required=True, help="Target to scan")
@click.option(
    "--instruction",
    type=str,
    default="",
    help="Custom instruction for the scan",
)
@click.option(
    "--scan-mode",
    type=click.Choice(["quick", "standard", "deep"]),
    default="standard",
    help="Scan mode",
)
@click.option(
    "--backend-url",
    default="http://localhost:8000",
    help="Backend URL",
)
@click.option(
    "--model",
    type=str,
    default=None,
    help="LLM model",
)
@click.option(
    "--mock-tools",
    is_flag=True,
    help="Use mock tools",
)
def main(
    target: str,
    instruction: str,
    scan_mode: str,
    backend_url: str,
    model: Optional[str],
    mock_tools: bool,
) -> None:
    """Debug client for backend validation."""
    asyncio.run(
        run_debug_client(
            target=target,
            backend_url=backend_url,
            scan_mode=scan_mode,
            instruction=instruction,
            mock_tools=mock_tools,
            model=model,
        )
    )


if __name__ == "__main__":
    main()
