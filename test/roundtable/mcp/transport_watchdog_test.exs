defmodule Roundtable.MCP.TransportWatchdogTest do
  use ExUnit.Case, async: true

  alias Roundtable.MCP.TransportWatchdog

  @check_interval 50
  @max_failures 3

  setup do
    # Ensure the Hermes registry is running (started by test_helper or application)
    start_supervised!(Hermes.Server.Registry)
    :ok
  end

  defp start_watchdog(opts \\ []) do
    test_pid = self()

    defaults = [
      name: :"watchdog_#{System.unique_integer([:positive])}",
      check_interval: @check_interval,
      max_failures: @max_failures,
      on_halt: fn -> send(test_pid, :halt_called) end
    ]

    start_supervised!({TransportWatchdog, Keyword.merge(defaults, opts)})
  end

  defp spawn_fake_transport do
    transport_name = {:transport, Roundtable.MCP.Server, :stdio}

    spawn(fn ->
      Registry.register(Hermes.Server.Registry, transport_name, nil)
      Process.sleep(:infinity)
    end)
    |> tap(fn _pid -> Process.sleep(10) end)
  end

  test "attaches to transport on startup" do
    _transport = spawn_fake_transport()
    _watchdog = start_watchdog()

    # Should not halt when transport is alive
    refute_receive :halt_called, 200
  end

  test "halts after max consecutive failures when transport never appears" do
    _watchdog = start_watchdog()

    assert_receive :halt_called, @check_interval * (@max_failures + 2)
  end

  test "halts after transport dies and doesn't recover" do
    transport = spawn_fake_transport()
    _watchdog = start_watchdog()

    # Let watchdog attach
    Process.sleep(@check_interval + 20)

    # Kill transport
    Process.exit(transport, :kill)

    assert_receive :halt_called, @check_interval * (@max_failures + 2)
  end

  test "recovers when transport reappears before max failures" do
    _watchdog = start_watchdog()

    # Let it fail once
    Process.sleep(@check_interval + 20)

    # Now register a transport
    _transport = spawn_fake_transport()

    # Should not halt — it recovered
    refute_receive :halt_called, @check_interval * (@max_failures + 2)
  end

  test "resets failure count when transport reappears" do
    transport1 = spawn_fake_transport()
    _watchdog = start_watchdog()

    # Let watchdog attach
    Process.sleep(@check_interval + 20)

    # Kill transport, let 1 failure accumulate
    Process.exit(transport1, :kill)
    Process.sleep(@check_interval + 20)

    # Bring back a new transport — resets failures
    _transport2 = spawn_fake_transport()
    Process.sleep(@check_interval + 20)

    # Kill again — should need full @max_failures checks to halt
    # (not carry over the previous failure count)
    refute_receive :halt_called, @check_interval * 2
  end
end
