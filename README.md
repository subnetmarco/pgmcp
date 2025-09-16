[![ci](https://github.com/subnetmarco/pgmcp/actions/workflows/ci.yml/badge.svg)](https://github.com/subnetmarco/pgmcp/actions/workflows/ci.yml)

# PGMCP - PostgreSQL Model Context Protocol Server

PGMCP is a Model Context Protocol (MCP) server that provides AI assistants with safe, read-only access to PostgreSQL databases through natural language queries. It acts as a bridge between AI models and your database, allowing you to ask questions in plain English and receive structured SQL results.

## What is MCP?

The [Model Context Protocol](https://modelcontextprotocol.io/) is an open standard that enables AI assistants to securely connect to external data sources and tools. PGMCP implements this protocol specifically for PostgreSQL databases.

## Features

- **Natural Language to SQL**: Ask questions in plain English and get automatically generated PostgreSQL queries
- **Safe Read-Only Access**: Built-in guards prevent any write operations (INSERT, UPDATE, DELETE, etc.)
- **Text Search**: Search across all text columns in your database with a single query
- **Schema Caching**: Intelligent caching of database schema information for better performance
- **HTTP SSE Transport**: Uses Server-Sent Events for real-time communication
- **OpenAI Integration**: Leverages OpenAI's language models for SQL generation
- **Connection Pooling**: Efficient PostgreSQL connection management
- **Authentication**: Optional Bearer token authentication
- **Comprehensive Testing**: Unit and integration tests included

## Architecture

The project consists of two main components:

### Server (`server/main.go`)
- **MCP Server**: Implements the Model Context Protocol specification
- **Database Integration**: Connects to PostgreSQL using pgx/v5 driver
- **AI Integration**: Uses OpenAI API for natural language to SQL translation
- **Safety Guards**: Ensures only read-only operations are executed
- **Schema Introspection**: Automatically discovers and caches database schema
- **HTTP Transport**: Serves MCP over HTTP with Server-Sent Events

### Client (`client/main.go`)
- **MCP Client**: Command-line client for testing and demonstration
- **Multiple Query Support**: Can execute multiple questions in a single run
- **Search Functionality**: Supports free-text search across database
- **Multiple Output Formats**: Table, JSON, and CSV output formats
- **Enhanced Error Handling**: Detailed error messages with recovery suggestions
- **Verbose Mode**: Optional detailed output with SQL queries and connection status
- **Flexible Configuration**: Command-line flags and environment variables

## Database Schema

The project includes a sample e-commerce database schema (`schema.sql`) with:

- **Users**: Customer information
- **Items**: Product catalog with SKUs, titles, descriptions, and prices
- **Orders**: Customer orders with status tracking
- **Order Items**: Line items for each order
- **Invoices**: Billing information linked to orders

## Installation & Setup

### Prerequisites
- Go 1.23+
- PostgreSQL database
- OpenAI API key (optional, can use other compatible APIs)

### Environment Variables

**Required:**
- `DATABASE_URL`: PostgreSQL connection string

**Optional:**
- `OPENAI_API_KEY`: OpenAI API key for SQL generation
- `OPENAI_MODEL`: Model to use (default: "gpt-4o-mini")
- `OPENAI_BASE_URL`: Custom OpenAI-compatible API endpoint
- `HTTP_ADDR`: Server listen address (default: ":8080")
- `HTTP_PATH`: MCP endpoint path (default: "/mcp/sse")
- `AUTH_BEARER`: Bearer token for authentication
- `SCHEMA_TTL`: Schema cache TTL (default: "5m")
- `QUERY_TIMEOUT`: Query execution timeout (default: "25s")
- `MAX_ROWS`: Maximum rows per query (default: 200)
- `LOG_LEVEL`: Logging level (debug, info, warn, error)

### Build & Run

```bash
# Build server
go build -o pgmcp-server ./server

# Build client  
go build -o pgmcp-client ./client

# Set up database
export DATABASE_URL="postgres://user:password@localhost:5432/mydb"
psql $DATABASE_URL < schema.sql

# Run server
export OPENAI_API_KEY="your-api-key"
./pgmcp-server

# Test with client
./pgmcp-client -ask "What are the top selling products?" -ask "Show me recent orders"
```

## Usage Examples

### Natural Language Queries

```bash
# Product analysis
./pgmcp-client -ask "What are the most popular items by quantity sold?"

# Customer insights  
./pgmcp-client -ask "Which customers have the highest order totals?"

# Revenue analysis
./pgmcp-client -ask "Show me total revenue by month for the last 6 months"

# Inventory questions
./pgmcp-client -ask "What items haven't been ordered recently?"
```

### Text Search

```bash
# Search across all text fields
./pgmcp-client -search "USB cable"
./pgmcp-client -search "ada@example.com"
```

### Output Formats

```bash
# Table format (default) - clean, readable tables
./pgmcp-client -ask "Show me all users" -format table

# JSON format - structured data for processing
./pgmcp-client -ask "Show me all users" -format json

# CSV format - for spreadsheet import
./pgmcp-client -ask "Show me all users" -format csv
```

### Advanced Client Options

```bash
# Verbose output with SQL queries and connection details
./pgmcp-client -ask "Show recent orders" -verbose

# Custom timeout and server URL
./pgmcp-client -url "http://prod-server:8080/mcp/sse" -timeout 30s -ask "Query"

# Multiple queries in one run
./pgmcp-client -ask "Show users" -ask "Show orders" -search "cables"

# Using environment variables
export PGMCP_SERVER_URL="http://localhost:8080/mcp/sse"
export PGMCP_AUTH_BEARER="your-token"
./pgmcp-client -ask "Your query"
```

## API Tools

The MCP server exposes two tools:

### `ask`
Translates natural language questions into safe SQL queries and executes them.

**Parameters:**
- `query` (string): The question in plain English
- `max_rows` (int, optional): Maximum rows to return
- `dry_run` (bool, optional): Generate SQL without executing

**Response:**
- `sql`: The generated SQL query
- `rows`: Query results (array of objects)
- `note`: Additional information (model used, etc.)

### `search`
Performs free-text search across all text columns in the database.

**Parameters:**
- `q` (string): Search term
- `limit` (int, optional): Maximum results to return

**Response:**
- `sql`: The generated search SQL
- `rows`: Matching results with source table and column information

## Safety Features

- **Read-Only Enforcement**: Regex-based detection and blocking of write operations
- **Query Timeouts**: Configurable timeouts prevent long-running queries
- **Row Limits**: Automatic LIMIT clauses prevent excessive data retrieval
- **Transaction Isolation**: All queries run in read-only transactions
- **Schema Truncation**: Large schemas are truncated to prevent token overflow
- **Input Validation**: Comprehensive input sanitization and validation

## Testing

```bash
# Run unit tests
go test ./server -v

# Run integration tests (requires Docker)
go test ./server -tags=integration -v
```

The integration tests use Testcontainers to spin up a real PostgreSQL instance and test the complete flow from natural language query to database results.

## Contributing

The project follows standard Go conventions:
- Use `gofmt` for code formatting
- Write tests for new functionality  
- Follow the existing error handling patterns
- Update documentation for API changes

## License

This project is open source. Check the repository for specific license terms.

## Related Projects

- [Model Context Protocol](https://modelcontextprotocol.io/) - The underlying protocol specification
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) - Go implementation of MCP
- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver for Go

---

PGMCP makes your PostgreSQL database accessible to AI assistants through natural language, enabling powerful data analysis and reporting capabilities while maintaining security through read-only access controls.
