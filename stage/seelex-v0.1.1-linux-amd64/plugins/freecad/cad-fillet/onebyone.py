import FreeCAD as App
doc = App.getDocument('FlangeCoupling')

# Clean up
to_remove = [o for o in doc.Objects if 'F_' in o.Name or 'Test' in o.Name]
for o in to_remove:
    try: doc.removeObject(o.Name)
    except: pass
doc.recompute()

obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges

# Try single edge filleting
current_obj = obj
ok = 0
for i, e in enumerate(edges):
    if e.Length < 10:
        continue
    if ok >= 10:
        break
    name = f'F_{i}'
    fillet = doc.addObject('Part::Fillet', name)
    fillet.Base = current_obj
    fillet.Edges = [(1, 2.0, 2.0)]  # Always first edge of current shape
    doc.recompute()
    try:
        if fillet.Shape and fillet.Shape.isValid() and fillet.Shape.Volume > 0:
            for old in [o for o in doc.Objects if o.Name != name and o.Name.startswith('F_') and o != fillet]:
                try: doc.removeObject(old.Name)
                except: pass
            current_obj = fillet
            ok += 1
            print(f'  Edge {i}: OK (vol={fillet.Shape.Volume:.0f})')
        else:
            doc.removeObject(name)
    except Exception as e:
        doc.removeObject(name)
        # print(f'  Edge {i}: FAIL')

print(f'Filleted {ok} edges one by one')
current_obj.Label = 'FlangeFinal'
try:
    vol = current_obj.Shape.Volume
    print(f'Final vol: {vol:.1f} mm3')
except Exception as e:
    print(f'Vol err: {e}')
