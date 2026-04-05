defmodule Roundtable.MCP.TransportWatchdogTest.FakeTransport do
  @moduledoc false
  use GenServer

  def start(name) do
    GenServer.start(__MODULE__, name)
  end

  @impl true
  def init(name) do
    Registry.register(Hermes.Server.Registry, name, nil)
    {:ok, %{}}
  end

  @impl true
  def handle_info(_msg, state), do: {:noreply, state}
end

defmodule Roundtable.MCP.TransportWatchdogTest do
  use ExUnit.Case, async: true

  alias Roundtable.MCP.TransportWatchdog
  alias Roundtable.MCP.TransportWatchdogTest.FakeTransport

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
    {:ok, pid} = FakeTransport.start(transport_name)
    Process.sleep(10)
    pid
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

  test "halts when transport is alive but unresponsive to sys.get_status" do
    transport_name = {:transport, Roundtable.MCP.Server, :stdio}

    stuck_transport =
      spawn(fn ->
        Registry.register(Hermes.Server.Registry, transport_name, nil)

        receive do
          :never -> :ok
        end
      end)

    Process.sleep(10)

    _watchdog = start_watchdog(liveness_timeout: 50)

    assert_receive :halt_called, @check_interval * (@max_failures + 3)

    Process.exit(stuck_transport, :kill)
  end
end
