[![ci](https://github.com/subnetmarco/pgmcp/actions/workflows/ci.yml/badge.svg)](https://github.com/subnetmarco/pgmcp/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/subnetmarco/pgmcp)](https://goreportcard.com/report/github.com/subnetmarco/pgmcp)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# PGMCP - PostgreSQL Model Context Protocol Server

PGMCP is a Model Context Protocol (MCP) server that provides AI assistants with safe, read-only access to **any PostgreSQL database** through natural language queries. It acts as a bridge between AI models and your database, allowing you to ask questions in plain English and receive structured SQL results with robust error handling and performance optimization.

## What is MCP?

The [Model Context Protocol](https://modelcontextprotocol.io/) is an open standard that enables AI assistants to securely connect to external data sources and tools. PGMCP implements this protocol specifically for PostgreSQL databases and **works with any MCP-compatible client** including Cursor, Claude Desktop, VS Code extensions, and custom integrations.

## Architecture Overview

```
👤 User / AI Assistant
         │
         │ "Who are the top customers?"
         ▼
┌─────────────────────────────────────────────────────────────┐
│                    Any MCP Client                           │
│                                                             │
│  PGMCP CLI  │  Cursor  │  Claude Desktop  │  VS Code  │ ... │
│  JSON/CSV   │  Chat    │  AI Assistant    │  Editor   │     │
└─────────────────────────────────────────────────────────────┘
         │
         │ HTTP SSE / MCP Protocol
         ▼
┌─────────────────────────────────────────────────────────────┐
│                    PGMCP Server                             │
│                                                             │
│  🔒 Security    🧠 AI Engine      🌊 Streaming               │
│  • Input Valid  • Schema Cache    • Auto-Pagination         │
│  • Audit Log    • OpenAI API      • Memory Management       │
│  • SQL Guard    • Error Recovery  • Connection Pool         │
└─────────────────────────────────────────────────────────────┘
         │
         │ Read-Only SQL Queries
         ▼
┌─────────────────────────────────────────────────────────────┐
│                Your PostgreSQL Database                     │
│                                                             │
│  Any Schema: E-commerce, Analytics, CRM, etc.               │
│  Tables • Views • Indexes • Functions                       │
└─────────────────────────────────────────────────────────────┘

External AI Services:
OpenAI API • Anthropic • Local LLMs (Ollama, etc.)

Key Benefits:
✅ Works with ANY PostgreSQL database (no assumptions about schema)
✅ No schema modifications required  
✅ Read-only access (100% safe)
✅ Automatic streaming for large results
✅ Intelligent query understanding
✅ Robust error handling (graceful AI failure recovery)
✅ Production-ready security and performance
```

### Data Flow Example

1. **User Query**: `"Who are the top 5 customers by total spending?"`
2. **Client**: Sends MCP request to server
3. **Security**: Validates and sanitizes input
4. **AI Translation**: OpenAI converts to SQL:
   ```sql
   SELECT u.first_name || ' ' || u.last_name as customer, 
          SUM(o.total_cents) as total_spent
   FROM users u JOIN orders o ON u.id = o.user_id
   GROUP BY u.id, u.first_name, u.last_name
   ORDER BY total_spent DESC LIMIT 5
   ```
5. **Schema Cache**: Provides table structure for context
6. **Streaming Engine**: Executes query with automatic pagination
7. **Database**: Returns results efficiently
8. **Client**: Displays formatted results in table/JSON/CSV
9. **Audit**: Logs query and results for security

## Features

- **Natural Language to SQL**: Ask questions in plain English and get automatically generated PostgreSQL queries
- **Intelligent Query Understanding**: Distinguishes between singular ("the user") and plural ("the users") questions for appropriate result sizing
- **Automatic Streaming**: Server automatically fetches and streams large result sets without user pagination
- **Safe Read-Only Access**: Built-in guards prevent any write operations (INSERT, UPDATE, DELETE, etc.)
- **Text Search**: Search across all text columns in your database with a single query
- **Schema Caching**: Intelligent caching of database schema information for better performance
- **HTTP SSE Transport**: Uses Server-Sent Events for real-time communication
- **OpenAI Integration**: Leverages OpenAI's language models for SQL generation
- **Connection Pooling**: Efficient PostgreSQL connection management with configurable limits
- **Enhanced Security**: Input sanitization, audit logging, and request size limits
- **Robust Error Handling**: Graceful handling of AI-generated SQL errors with helpful user feedback
- **Universal Database Support**: Works with ANY PostgreSQL database - no schema assumptions or modifications required
- **AI Safety Features**: Intelligent handling of unpredictable AI behavior with fallback strategies
- **Performance Protection**: Automatic detection and simplification of expensive queries
- **Graceful Shutdown**: Signal handling and proper resource cleanup
- **Configuration Validation**: Comprehensive validation with helpful error messages
- **Multiple Output Formats**: Table, JSON, and CSV output with beautiful formatting
- **Authentication**: Optional Bearer token authentication
- **Comprehensive Testing**: Unit, integration, security, and performance tests included

## Architecture

The project consists of two main components:

### Server (`server/main.go`)
- **MCP Server**: Implements the Model Context Protocol specification
- **Database Integration**: Connects to PostgreSQL using pgx/v5 driver with connection pooling
- **AI Integration**: Uses OpenAI API for natural language to SQL translation with intelligent query understanding
- **Automatic Streaming**: Fetches large result sets across multiple pages automatically
- **Safety Guards**: Ensures only read-only operations with input sanitization and audit logging
- **Robust Error Recovery**: Gracefully handles AI mistakes with helpful error messages instead of system crashes
- **Performance Intelligence**: Detects expensive queries and provides optimization suggestions
- **Schema Introspection**: Automatically discovers and caches database schema with performance indexes
- **HTTP Transport**: Serves MCP over HTTP with Server-Sent Events, request size limits, and graceful shutdown
- **Configuration Management**: Comprehensive validation with helpful error messages and environment variable support

### Client (`client/main.go`)
- **MCP Client**: Command-line client with enhanced user experience
- **Multiple Query Support**: Can execute multiple questions in a single run
- **Search Functionality**: Supports free-text search across database
- **Multiple Output Formats**: Beautiful table, JSON, and CSV output with automatic formatting
- **Streaming Support**: Displays large result sets as they're streamed from server
- **Enhanced Error Handling**: Detailed error messages with recovery suggestions and error categorization
- **Verbose Mode**: Optional detailed output with SQL queries, connection status, and streaming progress
- **Flexible Configuration**: Command-line flags and environment variables with validation

## Example Database Schema

PGMCP works with **any PostgreSQL database**. The project includes a comprehensive Amazon-like marketplace schema (`schema.sql`) as a demonstration with realistic data distribution:

### **Core Tables**
- **Users** (5,000): Customer profiles with Prime status, addresses, and geographic distribution
- **"Categories"** (23): Hierarchical product categories (Electronics > Computers > Laptops) - *demonstrates mixed-case table handling*
- **Brands** (20): Major brands like Apple, Samsung, Nike, etc.
- **Items** (1,800): Products with rich metadata (ratings, reviews, inventory, pricing)
- **Sellers** (500): Marketplace sellers with business profiles and ratings

### **Transaction Tables**
- **Orders** (10,000): Realistic order lifecycle with shipping, tax, and payment tracking
- **Order Items** (40,000): Line items with seller-specific pricing and conditions
- **Invoices** (9,500): Payment tracking with transaction IDs and status

### **Engagement Tables**
- **Reviews** (thousands): Customer reviews with ratings, helpful votes, and verified purchases
- **Wishlists**: Customer wish lists with public/private settings
- **Cart Items**: Active shopping carts
- **Product Views**: Analytics for recommendations

### **Realistic Data Distribution**
- **Power Users**: 5% of users make 15-25 orders each
- **Bestsellers**: 5% of products appear in 20-25 orders
- **Review Patterns**: Some users review 90% of purchases, others rarely review
- **Stock Variability**: From out-of-stock (0) to high inventory (25)
- **Seller Competition**: Popular items have 8-25 sellers, niche items have 1-3

### **Performance Features**
- **Full-text search indexes** on product titles and descriptions
- **Optimized indexes** for common query patterns
- **Realistic pricing** by category ($10 books to $3,000 laptops)
- **Geographic distribution** across US cities

### **Connecting to Your Own Database**

PGMCP requires **no schema modifications** and works with any PostgreSQL database:

```bash
# Point to your existing database
export DATABASE_URL="postgres://user:pass@your-db-host:5432/your_database"

# Start PGMCP server
./pgmcp-server

# Ask questions about your data
./pgmcp-client -ask "How many records are in my largest table?"
./pgmcp-client -ask "What tables do I have?"
./pgmcp-client -search "customer"
```

## Robust AI Error Handling

PGMCP is designed to handle the inherent unpredictability of AI systems gracefully:

### **When AI Makes Mistakes**

AI models sometimes generate SQL that references non-existent columns or tables. Instead of crashing, PGMCP:

✅ **Detects the Error**: Catches database errors like "column does not exist"  
✅ **Provides Helpful Feedback**: Returns user-friendly error messages with suggestions  
✅ **Shows Transparency**: Displays the actual SQL that was generated for learning  
✅ **Maintains Service**: System continues operating normally  
✅ **Logs Everything**: Comprehensive audit trail for debugging  

### **Example Error Handling**

**User Query**: `"Give me the user that purchased the most items"`

**AI Generated SQL**: `SELECT user_id FROM order_items...` *(incorrect - user_id doesn't exist in order_items)*

**PGMCP Response**:
```json
{
  "error": "Column not found in generated query",
  "suggestion": "Try rephrasing your question or ask about specific tables",
  "original_sql": "SELECT user_id, COUNT(*) FROM order_items GROUP BY user_id...",
  "note": "query failed - column not found"
}
```

This approach makes PGMCP **production-ready** for real-world use where AI behavior can be unpredictable.

**Supported Database Types:**
- **E-commerce platforms** (orders, products, customers)
- **Analytics databases** (events, metrics, time-series)
- **CRM systems** (contacts, deals, activities)
- **Financial systems** (transactions, accounts, reports)
- **Content management** (articles, users, comments)
- **IoT data** (sensors, readings, devices)
- **Any PostgreSQL schema** with tables and relationships

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

### Intelligent Natural Language Queries

The AI understands linguistic nuances for precise results with ANY database:

```bash
# SINGULAR queries return 1 result
./pgmcp-client -ask "Who is the person with the most records?" -format table
./pgmcp-client -ask "What is the largest entry by value?" -format table

# PLURAL queries return multiple results  
./pgmcp-client -ask "Who are the most active users?" -format table
./pgmcp-client -ask "What are the recent entries?" -format table

# COUNT queries return aggregations
./pgmcp-client -ask "How many records are in the main table?" -format table
./pgmcp-client -ask "What's the total count by category?" -format table
```

### Automatic Streaming for Large Results

```bash
# Server automatically streams large datasets from any table
./pgmcp-client -ask "Show me all records" -max-rows 500 -format table -verbose

# Stream with different formats
./pgmcp-client -ask "List all entries from this year" -format csv -max-rows 1000

# Complex analytics queries work with any schema
./pgmcp-client -ask "Show me trends by month" -format table -verbose
```

### Generic Database Queries

Works with any PostgreSQL schema:

```bash
# Data exploration
./pgmcp-client -ask "What tables do I have?" -format table
./pgmcp-client -ask "Show me the schema structure" -format table

# Relationship analysis
./pgmcp-client -ask "Which records are linked to the most other records?" -format table
./pgmcp-client -ask "What are the foreign key relationships?" -format table

# Data quality checks
./pgmcp-client -ask "Which records have missing values?" -format table
./pgmcp-client -ask "What are the data types of each column?" -format table

# Analytics (adapts to your domain)
./pgmcp-client -ask "Show me the top 10 by any numeric column" -format table
./pgmcp-client -ask "What are the most recent entries?" -format table
```

### Text Search Across Any Database

```bash
# Search across all text fields in your database
./pgmcp-client -search "john" -format table
./pgmcp-client -search "error" -format table  
./pgmcp-client -search "2024" -format table
```

### Output Formats & Streaming

```bash
# Beautiful table format with auto-streaming
./pgmcp-client -ask "Show me all data" -format table -verbose

# JSON format for API integration
./pgmcp-client -ask "Get summary statistics" -format json -max-rows 1000

# CSV format for data export
./pgmcp-client -ask "Export all records" -format csv -max-rows 5000
```

### Production Configuration

```bash
# Environment-based configuration for any database
export PGMCP_SERVER_URL="http://prod-server:8080/mcp/sse"
export PGMCP_AUTH_BEARER="your-secure-token"
export DATABASE_URL="postgres://user:pass@db-host:5432/your_database"

# Production queries work with any schema
./pgmcp-client -ask "Generate daily report" -format table -verbose -max-rows 10000
```

## Integration with MCP Clients

PGMCP can be integrated with any MCP-compatible client, including Cursor, Claude Desktop, and other AI assistants.

### **Cursor Integration**

Add PGMCP to your Cursor settings to give AI access to your database:

1. **Start PGMCP Server**:
```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/your_db"
export OPENAI_API_KEY="your-openai-key"
./pgmcp-server
```

2. **Configure Cursor** (Settings → Extensions → MCP):
```json
{
  "mcp.servers": {
    "pgmcp": {
      "transport": {
        "type": "sse",
        "url": "http://localhost:8080/mcp/sse"
      }
    }
  }
}
```

3. **Use in Cursor**:
```
You: "Can you analyze our customer data and show me the top spending customers?"

Cursor AI: I'll help you analyze your customer data using the database.
[Uses PGMCP to query your database automatically]
[Shows results in a formatted table]
```

### **Claude Desktop Integration**

Add to your Claude Desktop configuration:

**Edit config file** (`~/.config/claude-desktop/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "pgmcp": {
      "command": "curl",
      "args": [
        "-N", 
        "-H", "Accept: text/event-stream",
        "http://localhost:8080/mcp/sse"
      ]
    }
  }
}
```

### **Production Deployment for AI Assistants**

**Secure Setup:**
```bash
# Use authentication
export AUTH_BEARER="your-secure-random-token"

# Production configuration
export DATABASE_URL="postgres://readonly_user:pass@prod-db:5432/analytics"
export OPENAI_API_KEY="your-production-key"
export LOG_LEVEL="info"
export HTTP_ADDR="localhost:8080"  # Behind reverse proxy
```

**Docker Deployment:**
```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o pgmcp-server ./server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/pgmcp-server .
CMD ["./pgmcp-server"]
```

**MCP Client Config with Auth:**
```json
{
  "mcpServers": {
    "production-db": {
      "transport": {
        "type": "sse",
        "url": "https://your-domain.com/mcp/sse",
        "headers": {
          "Authorization": "Bearer your-production-token"
        }
      }
    }
  }
}
```

### **AI Assistant Capabilities**

Once integrated, AI assistants can:

- **Query your database** in natural language
- **Generate insights** from your data
- **Create reports** and analytics
- **Search across** all your data
- **Handle large datasets** with automatic streaming

**Example AI Conversations:**
```
User: "What trends do you see in my data?"
AI: [Analyzes database] "I see increasing activity over the past month..."

User: "Find records that haven't been updated recently"  
AI: [Queries database] "I found 247 records with no updates in 90 days..."

User: "Export my main dataset to CSV"
AI: [Streams large dataset] "Here's your complete dataset exported..."

User: "What's the relationship between these tables?"
AI: [Examines schema] "These tables are connected through foreign keys..."
```

## API Tools

The MCP server exposes three powerful tools:

### `ask`
Translates natural language questions into safe SQL queries and automatically streams all results.

**Features:**
- **Intelligent Query Understanding**: Distinguishes singular vs plural questions
- **Automatic Streaming**: Fetches large result sets across multiple pages
- **Memory Efficient**: Processes results in 50-row chunks

**Parameters:**
- `query` (string): The question in plain English
- `max_rows` (int, optional): Maximum total rows to return (default: 1000)
- `dry_run` (bool, optional): Generate SQL without executing

**Response:**
- `sql`: The generated SQL query
- `rows`: Complete query results (automatically streamed)
- `note`: Additional information (model used, pages streamed, etc.)

**Examples:**
```bash
# Singular query → 1 result
"Who is the record with the highest value?"

# Plural query → Multiple results  
"What are the most recent entries?"

# Large dataset → Auto-streamed
"Show me all records" (automatically streams large datasets)
```

### `search`
Performs free-text search across all text columns in the database.

**Parameters:**
- `q` (string): Search term
- `limit` (int, optional): Maximum results to return

**Response:**
- `sql`: The generated search SQL
- `rows`: Matching results with source table and column information

### `stream`
Advanced streaming tool for very large result sets with explicit pagination control.

**Parameters:**
- `query` (string): Natural language question
- `max_pages` (int, optional): Maximum pages to fetch (default: 10)
- `page_size` (int, optional): Results per page (default: 50)

**Response:**
- `sql`: The generated SQL query
- `pages`: Array of page objects with results
- `total_rows`: Total number of results
- `total_pages`: Total number of pages available

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

## Common Use Cases

### **Data Exploration**
- "What tables do I have in this database?"
- "Show me the schema structure"
- "What are the column types in [table_name]?"
- "How many records are in each table?"

### **Relationship Analysis**
- "What are the foreign key relationships?"
- "Which tables are connected to [table_name]?"
- "Show me records that reference [specific_id]"

### **Data Quality & Insights**
- "Which records have missing values?"
- "What are the most recent entries?"
- "Show me duplicate records"
- "What's the data distribution by [column_name]?"

### **Aggregation & Analytics**
- "What are the top 10 records by [numeric_column]?"
- "Show me trends by month/week/day"
- "Group records by [category_column]"
- "Calculate totals and averages"

### **Search & Discovery**
- "Find records containing [search_term]"
- "Show me records created this month"
- "What records match [specific_criteria]?"
- "List all unique values in [column_name]"

### **Reporting & Export**
- "Generate a summary report"
- "Export data for [specific_criteria]"
- "Create a CSV of all [table_name] records"
- "Show me statistics for [time_period]"

## Testing & Quality Assurance

PGMCP includes comprehensive test coverage to ensure reliability and performance:

### **Test Suite Coverage**
- **30+ Unit Tests**: Core functionality, SQL validation, error handling, pagination
- **Integration Tests**: End-to-end database operations, schema loading, MCP protocol compliance  
- **Security Tests**: Input sanitization, SQL injection protection, audit logging
- **Performance Tests**: Concurrent safety, memory usage, query optimization
- **Error Handling Tests**: AI failure scenarios, graceful degradation, user feedback

### **Running Tests**
```bash
# Unit tests (no database required)
go test ./server -v

# Integration tests (requires PostgreSQL)
go test ./server -tags=integration -v

# Performance benchmarks
go test ./server -tags=integration -bench=. -v

# All tests with coverage
go test ./... -cover -v
```

### **CI/CD Pipeline**
- **Build Verification**: Server and client compilation checks
- **Code Quality**: Linting with `go vet` and `gofmt`
- **Comprehensive Testing**: All test suites run automatically
- **Multi-version Support**: Tested across Go and PostgreSQL versions

## Troubleshooting

### **Connection Issues**
```bash
# Test server health
curl http://localhost:8080/healthz

# Check server logs
LOG_LEVEL=debug ./pgmcp-server

# Test database connection
psql "$DATABASE_URL" -c "SELECT 1"
```

### **MCP Integration Issues**
```bash
# Verify MCP endpoint
curl -H "Accept: text/event-stream" http://localhost:8080/mcp/sse

# Check client configuration
./pgmcp-client -ask "test query" -verbose
```

### **AI Query Issues**

If the AI generates incorrect SQL, PGMCP handles it gracefully:

```json
// Example: Column not found error
{
  "error": "Column not found in generated query",
  "suggestion": "Try rephrasing your question or ask about specific tables",
  "original_sql": "SELECT non_existent_column FROM table...",
  "note": "query failed - column not found"
}
```

**Common Solutions:**
- **Rephrase your question** to be more specific about table/column names
- **Ask about schema first**: `"What columns are in the [table_name] table?"`
- **Use simpler queries**: Break complex questions into smaller parts
- **Check the generated SQL** in the response to understand what went wrong

### **Performance Optimization**
```bash
# Increase connection pool for high concurrency
export MAX_ROWS=100  # Limit result size
export QUERY_TIMEOUT=60s  # Increase timeout for complex queries
export SCHEMA_TTL=30m  # Cache schema longer
```

PGMCP makes your PostgreSQL database accessible to AI assistants through natural language, enabling powerful data analysis and reporting capabilities while maintaining security through read-only access controls.
