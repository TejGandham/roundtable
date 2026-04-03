exclude =
  case :os.type() do
    {:win32, _} -> [:unix, :linux]
    {:unix, :linux} -> []
    _ -> [:linux]
  end

ExUnit.start(exclude: exclude)
