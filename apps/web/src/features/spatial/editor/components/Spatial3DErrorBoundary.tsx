"use client";

import { Component, type ReactNode } from "react";

// Isolates Canvas/WebGL initialization failures to the 3D viewport only —
// SpatialEditor and 2D editing must remain fully usable regardless of
// whether this device/browser can run WebGL (plan §"WebGL failure
// fallback"). Deliberately local to Spatial3DViewport, not a global
// app-level boundary.
export class Spatial3DErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      return (
        <div className="flex h-full min-h-96 flex-col items-center justify-center gap-2 rounded-lg border bg-muted/20 p-8 text-center">
          <p className="font-heading text-base font-semibold">3D view unavailable on this device or browser</p>
          <p className="max-w-md text-sm text-muted-foreground">Switch to the 2D floor plan to continue editing.</p>
        </div>
      );
    }
    return this.props.children;
  }
}
