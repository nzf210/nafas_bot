#!/bin/bash
# Script to auto-update database visualization
# Run this after any migration changes

echo "Updating database schema documentation..."
# In a real setup, this could use a tool like schemaspy or tbls
# For now, it reminds developers to update the mermaid diagram

echo "Please ensure database/schema.mermaid is updated to reflect new migrations."
cat database/schema.mermaid
