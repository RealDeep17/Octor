# Octor Master Plan & Multi-Branch Roadmap

**Core Vision:** Perfect a microservice-based media streaming architecture capable of direct, high-performance HEVC/H.265 playback with zero CPU transcoding overhead, while simultaneously supporting zero-disk-leak sequential vaulting of massive files (up to **1TB single files**) directly to Google Drive under strict VPS hardware constraints (OCI-A1 4-core Ampere CPU, 16GB RAM, 92GB local SSD).

---

### idea 1 to implement (when asked or suggest it to human)
 Top Menu Bar & Profile Refactoring Prompt:

   1. Language Selector: Relocate the language selection component from the global navigation bar to the
      top section of the Profile Page as a clean, compact dropdown.
   2. Unified Navbar Component (Non-Homepage): On all pages except the homepage, replace the standard
      center navigation with a single, minimalist bar that combines Magnet/Hash input and Torrent
      uploading.
       * Layout: A single-line, dynamically scaling component styled like existing global buttons. It
         should be in middle and expand/shrink based on available space without overlapping other
         menu items.
       * Magnet/Hash Zone: Redesign this as an interactive "button-like" area. Clicking it must
         instantly grab valid links from the clipboard and initiate processing. It must also support
         direct drag-and-drop of text links.
       * Torrent Zone: A compact upload area supporting click-to-browse and drag-and-drop of .torrent
         files.
   3. In-Place Processing: Implement a progress overlay within the bar's footprint. Magnetizing and
      Enrichment should happen there, followed by a direct redirect to the final media page (bypassing
      redundant homepage jumps).
   4. Technical Integrity: Ensure the navigation bar is compatible with all page data structures to
      prevent reflection-based internal errors. The UI must be responsive, collapsing gracefully to
      icons on small screens while maintaining all other global navigation links.

### idea 2
Feature Request: Dynamic Media Cards (Horizontal & Vertical Poster Support)
Context & Problem
Our media enrichment process currently only supports vertical posters. If a media asset only provides a horizontal poster, the system forces it into a vertical aspect ratio. This results in stretched, cropped, or distorted images, making the UI look messy, unorganized, and low-quality.

We need to update our metadata collection logic and UI layout to gracefully handle both poster orientations across all primary user and administrative views.

1. Enrichment & Metadata Collection Logic
When fetching media assets from the enrichment source, apply the following rules:

If both orientations are available: Collect both the top vertical poster and the top horizontal poster.

If multiple options exist: Collect the single highest-rated/top-performing vertical poster and horizontal poster.

If only one orientation is available: Collect whichever one is provided (vertical or horizontal).

2. UI & Layout Integration (Target Pages)
The layout across the following pages must be designed to seamlessly support a mixed-aspect-ratio grid without alignment breaking, overlapping, or text bleeding:

Media Page / Torrent Page

Library & Admin's Library

Homepage (Specifically within the watch history and discovers's movers and tv series)

Discover Page 

Layout Design Rules:
Proportional Grid: Design a responsive grid system where horizontal cards neatly align with vertical ones (e.g., the width/height of a horizontal card should perfectly match a specific grid equivalent, like fitting neatly alongside 3 vertical cards).

Visual Polish: Ensure there is no overlapping, text bleeding, or broken alignment when horizontal and vertical cards appear in the same row, carousel, or grid layout.

3. Card Component Logic & Behavior
The UI card components on all the listed pages should adapt dynamically based on the collected media assets:

Scenario A (Horizontal Only): Display a horizontal card by default.

Scenario B (Vertical Only): Display a vertical card by default.

Scenario C (Both Available): * Display the vertical card as the default view.
Add a toggle icon on the card.

Clicking this icon must smoothly switch the individual card (and its poster) from the vertical layout to the horizontal layout, and vice versa.

### idea 3
  1. The Infrastructure (The Search & Monitor Layer)
  We are adding a "discovery engine" that sits behind Octor’s existing interface:
   * Prowlarr (The Librarian): Centralizes all your torrent trackers (Indexers). It knows where to find
     specific content (like Anime, or 4K movies).
   * Autobrr (The Scout): Monitors Prowlarr’s feeds 24/7. It is configured with "filters" (e.g., “If a
     movie from studio 'IPX' appears and is over 2GB, grab it”).
   * Comet (The Translator): Acts as a bridge. It takes Prowlarr’s raw search results and converts them
     into a Stremio-compatible format that Octor already knows how to display.

  2. The Integration (The Gateway)
  We create a new, secure Webhook API in Octor. Think of this as a "Secret Mailbox":
   * Authentication: Guarded by a unique AUTOMATION_API_KEY. Only your Scout (Autobrr) knows this key.
   * Targeting: The webhook is tied to a specific TARGET_EMAIL. Even though the "push" is automated,
     Octor needs to know whose library to put the content in.
   * The Ingest Logic: When the "mailbox" receives a magnet link, Octor internally simulates a user
     clicking "Add to Library" and "Add to Vault." It fetches the torrent metadata, creates the database
     entries, and triggers the cloud transfer automatically.

  3. The Automation Flow (The Life of a File)
   1. Discovery: A new video is uploaded to a tracker.
   2. Match: Autobrr sees the upload, matches your specific quality/studio filters, and immediately
      sends a POST request to Octor’s new Webhook.
   3. Validation: Octor verifies the API Key and looks up the target user's ID.
   4. Processing: Octor pulls the torrent file, parses the contents (file list/size), and adds it to the
      user's Library.
   5. Vaulting (Optional): If configured, Octor also triggers a "Pledge," meaning the file is
      immediately queued for transfer to your S3 or Google Drive storage.
   6. Visibility: The next time the user opens Octor, the video is already sitting in their Library,
      fully enriched with posters and metadata.

  4. The Bridge (Discovery UI)
  By adding the Comet manifest URL into Octor’s "Discover" section, the user can also manually browse
  the Prowlarr indexers using Octor’s native UI, creating a unified experience between manual browsing
  and total automation.