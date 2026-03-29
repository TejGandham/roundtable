defmodule Roundtable.Prompt.Roles do
  @moduledoc "Loads role prompt files with project-local → global fallback."

  @spec load_role_prompt(String.t(), String.t(), String.t() | nil) :: String.t()
  def load_role_prompt(role_name, global_dir, project_dir \\ nil) do
    filename = "#{role_name}.txt"

    if project_dir do
      case File.read(Path.join(project_dir, filename)) do
        {:ok, content} ->
          content

        {:error, :enoent} ->
          load_from_global(role_name, global_dir, project_dir)

        {:error, reason} ->
          raise "Cannot read role file #{role_name} in project dir: #{inspect(reason)}"
      end
    else
      load_from_global(role_name, global_dir, nil)
    end
  end

  defp load_from_global(role_name, global_dir, project_dir) do
    filename = "#{role_name}.txt"

    case File.read(Path.join(global_dir, filename)) do
      {:ok, content} ->
        content

      {:error, :enoent} ->
        searched = if project_dir, do: project_dir, else: "none"
        raise "Role prompt not found: #{role_name} (searched #{searched}, #{global_dir})"

      {:error, reason} ->
        raise "Cannot read role file #{role_name} in global dir: #{inspect(reason)}"
    end
  end
end
