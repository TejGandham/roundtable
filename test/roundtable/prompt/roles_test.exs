defmodule Roundtable.Prompt.RolesTest do
  use ExUnit.Case, async: true

  alias Roundtable.Prompt.Roles

  @actual_roles_dir Path.expand("../../../roles", __DIR__)

  test "loads default role from global dir" do
    content = Roles.load_role_prompt("default", @actual_roles_dir, nil)

    assert is_binary(content)
    assert String.length(content) > 0
  end

  test "loads planner role from global dir" do
    content = Roles.load_role_prompt("planner", @actual_roles_dir, nil)

    assert String.contains?(content, "architect") or String.contains?(content, "Conclusion")
  end

  test "loads codereviewer role from global dir" do
    content = Roles.load_role_prompt("codereviewer", @actual_roles_dir, nil)

    assert String.contains?(content, "severity") or String.contains?(content, "critical")
  end

  test "loads from project-local dir when file exists" do
    dir = unique_tmp_dir()
    File.write!(Path.join(dir, "myrole.txt"), "project local content")

    content = Roles.load_role_prompt("myrole", "/nonexistent_global", dir)

    assert content == "project local content"
  end

  test "falls back to global when project-local file not found" do
    dir = unique_tmp_dir()

    content = Roles.load_role_prompt("default", @actual_roles_dir, dir)

    assert is_binary(content)
    assert String.length(content) > 0
  end

  test "raises when role not found in either location" do
    assert_raise RuntimeError, ~r/Role prompt not found: nonexistent/, fn ->
      Roles.load_role_prompt("nonexistent", "/nonexistent1", "/nonexistent2")
    end
  end

  test "error message includes both searched paths" do
    err =
      assert_raise RuntimeError, fn ->
        Roles.load_role_prompt("badname", "/global_dir", "/project_dir")
      end

    assert err.message =~ "/project_dir"
    assert err.message =~ "/global_dir"
  end

  defp unique_tmp_dir do
    dir = Path.join(System.tmp_dir!(), "roundtable_roles_#{System.unique_integer([:positive])}")
    File.mkdir_p!(dir)
    dir
  end
end
