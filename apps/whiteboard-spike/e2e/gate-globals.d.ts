interface Window {
  __TUTORHUB_WHITEBOARD_GATE__?: {
    connectionStatus(): string;
    createRect(id: string, x?: number, y?: number, label?: string): string;
    createFixture(count: number): number;
    moveRect(id: string, x: number, y: number): void;
    deleteShape(id: string): void;
    undo(): void;
    redo(): void;
    forceCreateRect(id: string): string;
    evidence(): {
      digest: string;
      shapeCount: number;
      shapes: Array<Record<string, unknown>>;
    };
    goOffline(): void;
    goOnline(): void;
    restart(): void;
  };
  __TUTORHUB_WHITEBOARD_LOAD_GATE__?: {
    connectedCount(): number;
    writerReady(): boolean;
    shapeCounts(): number[];
    createFixture(count: number): number;
  };
}
