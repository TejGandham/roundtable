#!/usr/bin/env node

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";

const server = new Server(
  { name: "fake-roundtable", version: "2.1.3" },
  { capabilities: { tools: {} } },
);

server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools: [] }));
server.setRequestHandler(CallToolRequestSchema, async (request) => ({
  content: [{
    type: "text",
    text: JSON.stringify({
      fixture: {
        provider: "fixture",
        status: "ok",
        response: `PI_PORT_OK:${request.params.name}`,
      },
      meta: { total_elapsed_ms: 1 },
    }),
  }],
}));

await server.connect(new StdioServerTransport());
