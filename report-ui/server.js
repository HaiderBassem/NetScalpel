const express = require('express');
const path = require('path');
const app = express();

const port = process.env.PORT || 5173;

// Serve the static files from the React app
app.use(express.static(path.join(__dirname, 'dist')));

// Serve the public folder explicitly (optional but good practice for external JSON files)
app.use(express.static(path.join(__dirname, 'public')));

// Handles any requests that don't match the ones above
app.get('*', (req, res) => {
  res.sendFile(path.join(__dirname, 'dist', 'index.html'));
});

app.listen(port, '0.0.0.0', () => {
  console.log(`Report UI server is running on port ${port}`);
});
