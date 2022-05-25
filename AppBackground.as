package com.limitlessprojects.repsteam.ui {

    import flash.display.Sprite;
    import flash.display.Shape;
    import flash.geom.Matrix;
    import flash.events.Event;
    import flash.events.MouseEvent; 
    import flash.display.Stage
    import com.greensock.TweenLite;
    
    public class AppBackground extends Sprite {

        public var window: Stage;
        public var colors: Array;

        public function AppBackground (_window: Stage, _colors: Array) {
            window = _window;
            colors = _colors;
            addEventListener(MouseEvent.MOUSE_DOWN, onDown, false, 0, true);
            this.addEventListener(Event.ADDED_TO_STAGE, onStage, false, 0, true);
        }

        public function onStage (e: Event): void {
            // trace(e)

            this.addEventListener(Event.ADDED_TO_STAGE, onStage, false, 0, true);
            this.alpha = 0;

            var matrix: Matrix = new Matrix();
            var shape: Shape = new Shape();
            
            // trace(getStageDimensions())

            matrix.createGradientBox(window.width, window.height * 1.2, 0.8);
            shape.graphics.beginGradientFill("linear", [colors[0], colors[1]], [1, 1], [0, 255], matrix);
            shape.graphics.drawRect(0, 0, window.width, window.height);
            
            addChild(shape);
            this.cacheAsBitmap = true;

            TweenLite.to(this, 0.3, { alpha: 1 });
        }

        public function onDown (e: MouseEvent): void {
            window.startMove();
        }

        public function onUp (e: MouseEvent): void {
            window.endMove();
        }

        private function getStageDimensions (stage: Stage): String {
            return "w: " + stage.stageWidth + " " + "h: " + stage.stageHeight
        }

    }
}