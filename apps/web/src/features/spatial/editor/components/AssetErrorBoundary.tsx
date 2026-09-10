"use client";

import { Component, type ReactNode } from "react";

export type AssetErrorBoundaryProps = {
  // Identity of the resolved descriptor currently being rendered
  // (`${descriptor.id}:${descriptor.version}`). A React error boundary's
  // tripped state is normally sticky — it never re-runs its children just
  // because props changed. Without this, a fixture whose resolved
  // descriptor later changes (a registry version bump) would stay stuck
  // on a stale error forever even if the NEW descriptor would load fine.
  assetKey: string;
  fallback: ReactNode;
  children: ReactNode;
};

type State = { failed: boolean; assetKey: string };

export default class AssetErrorBoundary extends Component<AssetErrorBoundaryProps, State> {
  state: State = { failed: false, assetKey: this.props.assetKey };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  static getDerivedStateFromProps(props: AssetErrorBoundaryProps, state: State): Partial<State> | null {
    if (props.assetKey !== state.assetKey) {
      // A new descriptor is being rendered — a stale error from the
      // PREVIOUS descriptor must never suppress a legitimate load of
      // this new one.
      return { failed: false, assetKey: props.assetKey };
    }
    return null;
  }

  render() {
    if (this.state.failed) {
      return this.props.fallback;
    }
    return this.props.children;
  }
}
