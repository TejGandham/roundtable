defmodule Roundtable.MCP.TransportWatchdog do
  @moduledoc false
  # Monitors the MCP stdio transport process. If it disappears and doesn't
  # recover within @max_failures consecutive checks, halts the BEAM so
  # Claude Code can restart a fresh MCP server.
  #
  # Without this, a stale BEAM process lingers under --no-halt after the
  # transport crashes — reporting as "Connected" while silently dropping
  # all tool calls.

  use GenServer
  require Logger

  @check_interval 5_000
  @max_failures 3

  def start_link(opts \\ []) do
    name = Keyword.get(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  @impl true
  def init(opts) do
    state = %{
      failures: 0,
      ref: nil,
      server: Keyword.get(opts, :server, Roundtable.MCP.Server),
      check_interval: Keyword.get(opts, :check_interval, @check_interval),
      max_failures: Keyword.get(opts, :max_failures, @max_failures),
      on_halt: Keyword.get(opts, :on_halt)
    }

    {:ok, state, {:continue, :attach}}
  end

  @impl true
  def handle_continue(:attach, state) do
    {:noreply, check_transport(state)}
  end

  @impl true
  def handle_info(:check, state) do
    {:noreply, check_transport(state)}
  end

  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{ref: ref} = state) do
    schedule_check(state)
    {:noreply, %{state | ref: nil, failures: 1}}
  end

  def handle_info(_msg, state) do
    {:noreply, state}
  end

  defp check_transport(state) do
    case Hermes.Server.Registry.whereis_transport(state.server, :stdio) do
      pid when is_pid(pid) ->
        if state.ref, do: Process.demonitor(state.ref, [:flush])
        ref = Process.monitor(pid)
        %{state | ref: ref, failures: 0}

      nil ->
        failures = state.failures + 1

        if failures >= state.max_failures do
          Logger.warning(
            "[roundtable] stdio transport gone for #{failures} consecutive checks, halting"
          )

          halt(state)
        end

        schedule_check(state)
        %{state | ref: nil, failures: failures}
    end
  end

  defp halt(%{on_halt: fun}) when is_function(fun, 0), do: fun.()
  defp halt(_state), do: System.halt(1)

  defp schedule_check(%{check_interval: interval}) do
    Process.send_after(self(), :check, interval)
  end
end
