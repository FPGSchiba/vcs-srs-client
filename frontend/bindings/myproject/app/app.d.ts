import { CancellablePromise } from '@wailsio/runtime';

export declare function Greet(name: string): CancellablePromise<string>;
export declare function OpenNewWindow(title: string): CancellablePromise<void>;
export declare function SetApp(app: any | null): CancellablePromise<void>;
