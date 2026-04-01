defmodule Mix.Tasks.Deps.Patch do
  @shortdoc "Apply local patches to dependencies"
  @moduledoc "Applies patches from patches/ to deps after `mix deps.get`."

  use Mix.Task

  @impl true
  def run(_args) do
    root = Mix.Project.deps_path() |> Path.dirname()
    patches_dir = Path.join(root, "patches")

    Path.wildcard(Path.join(patches_dir, "*.patch"))
    |> Enum.each(fn patch_file ->
      Mix.shell().info("Applying #{patch_file}...")

      case System.cmd("patch", ["-p1", "--forward", "--directory=#{Path.join(root, "deps/hermes_mcp")}", "-i", patch_file],
             stderr_to_stdout: true
           ) do
        {output, 0} ->
          Mix.shell().info(output)

        {output, _} ->
          if String.contains?(output, "already applied") or String.contains?(output, "Reversed") do
            Mix.shell().info("#{patch_file}: already applied, skipping")
          else
            Mix.shell().error("Failed to apply #{patch_file}:\n#{output}")
          end
      end
    end)
  end
end
